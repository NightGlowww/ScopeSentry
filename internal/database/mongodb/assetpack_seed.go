// Package mongodb -----------------------------
// @file      : assetpack_seed.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 23:58
// -------------------------------------------
package mongodb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/yaml.v3"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 资产包播种：把 constants 里内嵌的字典包与 POC 模板落成运行时数据（dictionary / PocList）。
//
// 与 initdb 里「只有全新库才播种」的默认资产不同，这里是可升级路径：
//   - config 集合里的 AssetPackVersion 标记是幂等闸，版本不变直接返回；
//   - 标记之外仍逐个检查存在性，运营删掉某个包不会被下次启动复活；
//   - 单个包失败只记日志并汇总返回，调用方（initdb）记日志后继续启动；
//     失败时不写版本标记，下次启动会重试，已成功的项由存在性检查跳过。
//
// 标记名是产品语义（"当前播种到哪个资产包内容"），不绑任何具体来源。
const assetPackMarkerName = "AssetPackVersion"

// SeedAssetPacks 播种内嵌资产包；重复调用安全。
func SeedAssetPacks(db *mongo.Database) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	configColl := db.Collection("config")
	var marker bson.M
	err := configColl.FindOne(ctx, bson.M{"name": assetPackMarkerName}).Decode(&marker)
	if err != nil && err != mongo.ErrNoDocuments {
		return fmt.Errorf("read asset pack marker: %w", err)
	}
	version, err := constants.AssetPackDigest()
	if err != nil {
		return fmt.Errorf("compute asset pack digest: %w", err)
	}
	if fmt.Sprintf("%v", marker["value"]) == version {
		return nil
	}

	dictAdded, dictSkipped, dictFailed := 0, 0, 0
	packs, packErr := constants.EmbeddedDictPacks()
	if packErr != nil {
		return fmt.Errorf("load embedded dict packs: %w", packErr)
	}
	for _, pack := range packs {
		added, err := seedOneDictPack(ctx, db, pack)
		switch {
		case err != nil:
			dictFailed++
			logger.Error(fmt.Sprintf("seed dict pack %s/%s failed: %v", pack.Kind, pack.Name, err))
		case added:
			dictAdded++
		default:
			dictSkipped++
		}
	}

	pocAdded, pocSkipped, pocFailed := 0, 0, 0
	templates, tplErr := constants.POCPacks()
	if tplErr != nil {
		// 内嵌模板解析失败属于构建产物损坏，必须让版本标记保持未写，等修好后重试
		return fmt.Errorf("load embedded poc pack: %w", tplErr)
	}
	for _, template := range templates {
		added, err := seedOnePOC(ctx, db, template)
		switch {
		case err != nil:
			pocFailed++
			logger.Error(fmt.Sprintf("seed poc %s failed: %v", template.ID, err))
		case added:
			pocAdded++
		default:
			pocSkipped++
		}
	}

	// 有失败就不写标记，下次启动重试；已成功写入的项由存在性检查跳过。
	if dictFailed > 0 || pocFailed > 0 {
		return fmt.Errorf("asset packs partially seeded (dict added=%d skipped=%d failed=%d, poc added=%d skipped=%d failed=%d)",
			dictAdded, dictSkipped, dictFailed, pocAdded, pocSkipped, pocFailed)
	}

	if _, err := configColl.UpdateOne(ctx,
		bson.M{"name": assetPackMarkerName},
		bson.M{"$set": bson.M{
			"name":  assetPackMarkerName,
			"value": version,
			"type":  "system",
		}},
		options.Update().SetUpsert(true),
	); err != nil {
		return fmt.Errorf("write asset pack marker: %w", err)
	}

	logger.Info(fmt.Sprintf("asset packs seeded: dict added=%d skipped=%d, poc added=%d skipped=%d (digest=%s)",
		dictAdded, dictSkipped, pocAdded, pocSkipped, version))
	return nil
}

// seedOneDictPack 写入单个字典包：meta + GridFS 内容。
// 行级索引不在这里建（entries_synced=false），首次预览/搜索时由 dictionary 服务回填，
// 这样启动路径不背 15 万行字典的写放大的账。
func seedOneDictPack(ctx context.Context, db *mongo.Database, pack constants.DictPack) (bool, error) {
	coll := db.Collection("dictionary")
	var existing bson.M
	err := coll.FindOne(ctx, bson.M{"name": pack.Name, "category": pack.Kind}).Decode(&existing)
	if err == nil {
		return false, nil
	}
	if err != mongo.ErrNoDocuments {
		return false, fmt.Errorf("check dictionary: %w", err)
	}

	content, err := constants.DictPackContent(pack)
	if err != nil {
		return false, err
	}
	if len(content) == 0 {
		return false, fmt.Errorf("empty dictionary content")
	}

	result, err := coll.InsertOne(ctx, bson.M{
		"name":           pack.Name,
		"category":       pack.Kind,
		"size":           fmt.Sprintf("%.2f", float64(len(content))/(1024*1024)),
		"line_count":     int64(pack.Lines),
		"entries_synced": false,
		"entry_seq":      int64(0),
		"updated_at":     time.Now(),
	})
	if err != nil {
		return false, fmt.Errorf("insert dictionary meta: %w", err)
	}
	id, ok := result.InsertedID.(primitive.ObjectID)
	if !ok {
		return false, fmt.Errorf("unexpected inserted id type %T", result.InsertedID)
	}
	if err := CreateGridFSFile(db, id.Hex(), content); err != nil {
		// 内容写入失败要把 meta 回滚，否则列表里会出现一个空字典
		if _, delErr := coll.DeleteOne(ctx, bson.M{"_id": result.InsertedID}); delErr != nil {
			logger.Error(fmt.Sprintf("rollback dictionary meta %s failed: %v", pack.Name, delErr))
		}
		return false, fmt.Errorf("write dictionary content: %w", err)
	}
	return true, nil
}

// seedOnePOC 写入单个 nuclei 模板；已存在同 template id 时跳过。
func seedOnePOC(ctx context.Context, db *mongo.Database, template constants.POCPack) (bool, error) {
	coll := db.Collection("PocList")
	var existing bson.M
	err := coll.FindOne(ctx, bson.M{"id": template.ID}).Decode(&existing)
	if err == nil {
		return false, nil
	}
	if err != mongo.ErrNoDocuments {
		return false, fmt.Errorf("check poc: %w", err)
	}

	var header models.PocTemplate
	if err := yaml.Unmarshal([]byte(template.Content), &header); err != nil {
		return false, fmt.Errorf("parse poc yaml: %w", err)
	}
	if header.Info.Name == "" {
		return false, fmt.Errorf("poc %s has empty info.name", template.ID)
	}
	level := strings.ToLower(strings.TrimSpace(header.Info.Severity))
	if level == "" {
		level = "unknown"
	}
	tags := make([]string, 0, 8)
	for _, tag := range strings.Split(header.Info.Tags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	if _, err := coll.InsertOne(ctx, bson.M{
		"name":    header.Info.Name,
		"content": template.Content,
		"id":      template.ID,
		"level":   level,
		"tags":    tags,
		"time":    time.Now().Format("2006-01-02 15:04:05"),
	}); err != nil {
		return false, fmt.Errorf("insert poc: %w", err)
	}
	return true, nil
}
