package mongodb

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/utils/random"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func printProgressBar(step, total int, action string) {
	width := 50
	progress := float64(step) / float64(total)
	bar := int(progress * float64(width))

	fmt.Printf("\r[%s%s] %d/%d %s\n",
		strings.Repeat("=", bar),
		strings.Repeat(" ", width-bar),
		step, total, action)
}

func CreateDatabase() error {
	client := Client
	if client == nil {
		return fmt.Errorf("MongoDB client is not initialized")
	}

	// 获取数据库列表
	databases, err := client.ListDatabaseNames(context.Background(), bson.M{})
	if err != nil {
		return fmt.Errorf("failed to list databases: %v", err)
	}

	db := client.Database(config.GlobalConfig.MongoDB.Database)

	// 如果数据库不存在，创建数据库
	if !contains(databases, config.GlobalConfig.MongoDB.Database) {
		totalSteps := 13
		currentStep := 0

		// 创建用户集合
		collection := db.Collection("user")
		password, err := random.GeneratePassword(16)
		if err != nil {
			password = random.GenerateRandomString(16)
		}

		// 打印重要信息
		separator := strings.Repeat("=", 50)
		fmt.Printf("%s\n", separator)
		fmt.Println("✨✨✨ IMPORTANT NOTICE: Please review the User/Password below ✨✨✨")
		fmt.Println(separator)
		fmt.Printf("🔑 User/Password: ScopeSentry/%s\n", password)
		fmt.Println(separator)
		fmt.Println("✅ Ensure the User/Password is correctly copied!")
		fmt.Println("✅ The initialization password is stored in the file PASSWORD")

		// 保存密码到文件
		if err := os.WriteFile(filepath.Join(config.GlobalConfig.System.ExeDir, "PASSWORD"), []byte(password), 0644); err != nil {
			return fmt.Errorf("failed to write password file: %v", err)
		}

		// 加密密码
		hashedPassword := helper.Sha256Hex(password)
		// 创建用户
		_, err = collection.InsertOne(context.Background(), bson.M{
			"username":   "ScopeSentry",
			"password":   hashedPassword,
			"role":       constants.RoleSuperAdmin,
			"status":     "active",
			"created_at": time.Now(),
			"updated_at": time.Now(),
		})
		if err != nil {
			return fmt.Errorf("failed to create user: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating user")

		// 创建配置集合
		configCollection := db.Collection("config")

		// 插入系统配置
		// 用 upsert：多个测试二进制/进程并发首启时不会插出两条 timezone
		_, err = configCollection.UpdateOne(context.Background(),
			bson.M{"name": "timezone"},
			bson.M{"$setOnInsert": bson.M{"name": "timezone", "value": "Asia/Shanghai", "type": "system"}},
			options.Update().SetUpsert(true),
		)
		if err != nil {
			return fmt.Errorf("failed to insert timezone config: %v", err)
		}
		configCollection.InsertOne(context.Background(), bson.M{
			"name":  "ModulesConfig",
			"value": constants.ModulesConfig,
			"type":  "system",
		})

		configCollection.InsertOne(context.Background(), bson.M{
			"name":  "SubfinderApiConfig",
			"value": constants.SubfinderApiConfig,
			"type":  "subfinder",
		})

		configCollection.InsertOne(context.Background(), bson.M{
			"name":  "RadConfig",
			"value": constants.RadConfig,
			"type":  "rad",
		})

		// 插入指纹版本
		configCollection.InsertOne(context.Background(), bson.M{
			"name":  "FingerVersion",
			"value": "2026-01-20 23:50",
			"type":  "finger",
		})

		currentStep++
		printProgressBar(currentStep, totalSteps, "Setting timezone")

		// 创建通知配置
		_, err = configCollection.InsertOne(context.Background(), bson.M{
			"name":                          "notification",
			"dirScanNotification":           true,
			"portScanNotification":          true,
			"sensitiveNotification":         true,
			"subdomainTakeoverNotification": true,
			"pageMonNotification":           true,
			"subdomainNotification":         true,
			"vulNotification":               true,
			"type":                          "notification",
		})
		if err != nil {
			return fmt.Errorf("failed to insert notification config: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Setting notification config")

		// 创建定时任务
		scheduledTasksCollection := db.Collection("ScheduledTasks")
		_, err = scheduledTasksCollection.InsertOne(context.Background(), bson.M{
			"id":      "page_monitoring",
			"name":    "Page Monitoring",
			"hour":    24,
			"node":    []string{},
			"allNode": true,
			"type":    "Page Monitoring",
			"state":   true,
		})
		if err != nil {
			return fmt.Errorf("failed to insert scheduled task: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating scheduled tasks")

		// 创建通知集合
		err = db.CreateCollection(context.Background(), "notification")
		if err != nil {
			return fmt.Errorf("failed to create notification collection: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating notification collection")

		// 创建字典集合
		dictionaryCollection := db.Collection("dictionary")
		// 插入目录扫描字典
		dirDict := constants.DirDict
		size := float64(len(dirDict)) / (1024 * 1024)
		result, err := dictionaryCollection.InsertOne(context.Background(), bson.M{
			"name":     "default",
			"category": "dir",
			"size":     fmt.Sprintf("%.2f", size),
		})
		if err != nil {
			return fmt.Errorf("failed to insert dir dictionary: %v", err)
		}
		// 使用GridFS存储字典内容
		if err := CreateGridFSFile(db, result.InsertedID.(primitive.ObjectID).Hex(), []byte(dirDict)); err != nil {
			return fmt.Errorf("failed to write dir dictionary: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating dir dictionary")

		// 插入子域名字典
		domainDict := constants.DomainDict
		size = float64(len(domainDict)) / (1024 * 1024)
		result, err = dictionaryCollection.InsertOne(context.Background(), bson.M{
			"name":     "default",
			"category": "subdomain",
			"size":     fmt.Sprintf("%.2f", size),
		})
		if err != nil {
			return fmt.Errorf("failed to insert subdomain dictionary: %v", err)
		}
		if err := CreateGridFSFile(db, result.InsertedID.(primitive.ObjectID).Hex(), []byte(domainDict)); err != nil {
			return fmt.Errorf("failed to write subdomain dictionary: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating subdomain dictionary")

		// 插入敏感信息规则
		sensitiveCollection := db.Collection("SensitiveRule")
		sensitiveData, _ := constants.GetSensitive()
		if len(sensitiveData) > 0 {
			_, err = sensitiveCollection.InsertMany(context.Background(), sensitiveData)
			if err != nil {
				return fmt.Errorf("failed to insert sensitive rules: %v", err)
			}
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating sensitive rules")

		// 插入默认端口
		portCollection := db.Collection("PortDict")
		portData, _ := constants.GetPort()
		_, err = portCollection.InsertMany(context.Background(), portData)
		if err != nil {
			return fmt.Errorf("failed to insert port dictionary: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating port dictionary")

		// 插入POC
		//pocCollection := db.Collection("PocList")
		//pocData := getPoc()
		//if len(pocData) > 0 {
		//	_, err = pocCollection.InsertMany(context.Background(), pocData)
		//	if err != nil {
		//		return fmt.Errorf("failed to insert POC: %v", err)
		//	}
		//}
		//currentStep++
		//printProgressBar(currentStep, totalSteps, "Creating POC")

		// 插入指纹规则
		fingerprintCollection := db.Collection("FingerprintRules")
		fingerprint, _ := constants.GetFingerprintData()
		if len(fingerprint) > 0 {
			_, err = fingerprintCollection.InsertMany(context.Background(), fingerprint)
			if err != nil {
				return fmt.Errorf("failed to insert fingerprint rules: %v", err)
			}
		}

		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating fingerprint rules")

		// 创建默认插件
		pluginsCollection := db.Collection("plugins")
		var plgDocs []interface{}
		for _, p := range constants.Plugins {
			plgDocs = append(plgDocs, p)
		}
		_, err = pluginsCollection.InsertMany(context.Background(), plgDocs)
		if err != nil {
			return fmt.Errorf("failed to insert plugins: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating plugins")

		// 创建默认扫描模板
		scanTemplateCollection := db.Collection("ScanTemplates")
		_, err = scanTemplateCollection.InsertOne(context.Background(), constants.ScanTemplateDefault)
		if err != nil {
			return fmt.Errorf("failed to insert scan template: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating scan template")

		// 创建索引
		if err := createIndexes(db); err != nil {
			return fmt.Errorf("failed to create indexes: %v", err)
		}
		currentStep++
		printProgressBar(currentStep, totalSteps, "Creating indexes")

		fmt.Println() // 换行
		logger.Info("Project initialization successful")
	} else {
		// 数据库已存在，检查并更新必要的配置
		configCollection := db.Collection("config")
		var result bson.M
		err := configCollection.FindOne(context.Background(), bson.M{"name": "timezone"}).Decode(&result)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				// 库存在但缺配置（历史库或并发首启的中间态）：补默认值，不要直接退出
				if _, insErr := configCollection.UpdateOne(context.Background(),
					bson.M{"name": "timezone"},
					bson.M{"$setOnInsert": bson.M{"name": "timezone", "value": "Asia/Shanghai", "type": "system"}},
					options.Update().SetUpsert(true),
				); insErr != nil {
					return fmt.Errorf("failed to seed default timezone config: %v", insErr)
				}
			} else {
				return fmt.Errorf("failed to get timezone config: %v", err)
			}
		}

		// 升级路径：用户管理启用后，历史库里的种子用户没有 role/status，会被权限中间件 403 挡死。
		// 只给内置的 ScopeSentry 补超管（其它用户不擅自提权），缺 status 的统一置 active。
		userColl := db.Collection("user")
		if _, err := userColl.UpdateOne(context.Background(),
			bson.M{"username": "ScopeSentry", "role": bson.M{"$in": []interface{}{"", nil}}},
			bson.M{"$set": bson.M{"role": constants.RoleSuperAdmin, "status": "active", "updated_at": time.Now()}},
		); err != nil {
			logger.Error(fmt.Sprintf("failed to backfill seed user role: %v", err))
		}
		if _, err := userColl.UpdateMany(context.Background(),
			bson.M{"status": bson.M{"$in": []interface{}{"", nil}}},
			bson.M{"$set": bson.M{"status": "active", "updated_at": time.Now()}},
		); err != nil {
			logger.Error(fmt.Sprintf("failed to backfill user status: %v", err))
		}

		// 升级路径：老库也要补新索引（createIndexes 对已存在的索引是 no-op）。
		// 之前只有全新安装才会建索引，导致升级后新集合/新索引永远缺失。
		if err := createIndexes(db); err != nil {
			logger.Error(fmt.Sprintf("failed to ensure indexes on existing database: %v", err))
		}

		// 检查定时任务
		scheduledTasksCollection := db.Collection("ScheduledTasks")
		var taskResult bson.M
		err = scheduledTasksCollection.FindOne(context.Background(), bson.M{"id": "page_monitoring"}).Decode(&taskResult)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				// 创建默认定时任务
				_, err = scheduledTasksCollection.InsertOne(context.Background(), bson.M{
					"id":    "page_monitoring",
					"name":  "Page Monitoring",
					"hour":  24,
					"type":  "Page Monitoring",
					"state": true,
				})
				if err != nil {
					return fmt.Errorf("failed to insert default scheduled task: %v", err)
				}
			} else {
				return fmt.Errorf("failed to check scheduled task: %v", err)
			}
		}
	}

	// 资产包播种（内嵌字典 / nuclei POC）：可升级路径，靠内容指纹做幂等。
	// 失败只记日志，不能因为一个坏包把启动流程卡死。
	if err := SeedAssetPacks(db); err != nil {
		logger.Error(fmt.Sprintf("failed to seed asset packs: %v", err))
	}

	return nil
}

// EnsureIndexes 幂等地补齐所有集合索引（createIndexes 对已存在的索引是 no-op）。
// 生产启动路径会调用它；集成测试在断言 explain 之前也先调一次，确保索引真的建出来了，
// 而不是「测试库没建索引 -> 计划退化成 COLLSCAN」的假阴性。
func EnsureIndexes(db *mongo.Database) error {
	return createIndexes(db)
}

// createUserIndexes 建立用户管理面（对标 ARL-Next user.py）需要的索引：
//
//   - user(created_at -1)：列表固定按创建时间倒序分页；
//   - user(role)：最后一个 super_admin 保护要按角色计数；
//   - user(username) 唯一：登录按用户名取人，创建时去重，并发由唯一索引兜底；
//   - user(email) 普通索引：FindByEmail / 列表精确匹配用。
//
// 为什么 email 不做唯一索引：历史库和测试库里都可能存在共用同一个 email 的账号
// （例如集成测试的种子用户统一用 e2e@example.invalid），唯一索引会直接把启动打成
// 「退化路径」，唯一性形同虚设还多一条误导性 warn。邮箱唯一性由业务层
// （services/user 的 FindByEmail 预检查 + 并发写入的重复键兜底）保证。
//
// 用户名唯一索引在历史库上也可能因为脏数据建不起来：这时退化成普通索引并记一条 warn，
// 不让启动直接失败——业务层仍然做存在性校验。退化索引用不同名字（user_username），
// 避免和已建成的唯一索引重名冲突。
func createUserIndexes(ctx context.Context, db *mongo.Database) error {
	coll := db.Collection("user")
	if _, err := coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "created_at", Value: -1}}, Options: options.Index().SetName("user_created_at")},
		{Keys: bson.D{{Key: "role", Value: 1}}, Options: options.Index().SetName("user_role")},
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetName("user_email")},
	}); err != nil {
		return fmt.Errorf("failed to create user list indexes: %v", err)
	}

	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetName("user_username_unique").SetUnique(true),
	}); err == nil {
		return nil
	} else {
		logger.Warn(fmt.Sprintf("user index user_username_unique unavailable, fallback to plain index: %v", err))
	}
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetName("user_username"),
	}); err != nil {
		return fmt.Errorf("failed to create user username index: %v", err)
	}
	return nil
}

func createIndexes(db *mongo.Database) error {
	// 创建页面监控索引
	pageMonitoring := db.Collection("PageMonitoring")
	_, err := pageMonitoring.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "url", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("failed to create PageMonitoring index: %v", err)
	}
	// 页面监控列表按 time 倒序分页，补上排序索引
	if _, err := pageMonitoring.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "time", Value: -1}},
	}); err != nil {
		return fmt.Errorf("failed to create PageMonitoring time index: %v", err)
	}

	// 创建页面监控内容索引
	pageMonitoringBody := db.Collection("PageMonitoringBody")
	_, err = pageMonitoringBody.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "md5", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("failed to create PageMonitoringBody index: %v", err)
	}

	// POC 索引：导入/同步按 template id 做「存在则比内容」，列表按 time 倒序分页。
	// 唯一索引在历史脏数据（重复 id）下会建不起来，退化为普通索引，业务侧仍按 id 比对更新。
	pocList := db.Collection("PocList")
	if _, err := pocList.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		if _, fallbackErr := pocList.Indexes().CreateOne(context.Background(), mongo.IndexModel{
			Keys: bson.D{{Key: "id", Value: 1}},
		}); fallbackErr != nil {
			return fmt.Errorf("failed to create PocList id index: %v", fallbackErr)
		}
		logger.Warn(fmt.Sprintf("PocList unique id index unavailable, fallback to plain index: %v", err))
	}
	if _, err := pocList.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "time", Value: -1}},
	}); err != nil {
		return fmt.Errorf("failed to create PocList time index: %v", err)
	}

	// 指纹规则索引：一键更新要按 fingerprint_id 判重，同名判重要按 name 查
	fingerprintRules := db.Collection("FingerprintRules")
	if _, err := fingerprintRules.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "fingerprint_id", Value: 1}},
		Options: options.Index().SetSparse(true),
	}); err != nil {
		return fmt.Errorf("failed to create FingerprintRules fingerprint_id index: %v", err)
	}
	// name 不做唯一索引：历史库可能已有重复，唯一索引会让启动直接失败
	if _, err := fingerprintRules.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "name", Value: 1}},
	}); err != nil {
		return fmt.Errorf("failed to create FingerprintRules name index: %v", err)
	}

	// 标签颜色覆盖：tag 唯一（issue #217）
	tagColor := db.Collection("TagColor")
	if _, err := tagColor.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "tag", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return fmt.Errorf("failed to create TagColor tag index: %v", err)
	}

	// 任务进度快照索引：按 taskID 查/删（issue #278）
	progressSnapshot := db.Collection("TaskProgressSnapshot")
	if _, err := progressSnapshot.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "taskID", Value: 1}},
	}); err != nil {
		return fmt.Errorf("failed to create TaskProgressSnapshot taskID index: %v", err)
	}

	// 创建资产索引
	asset := db.Collection("asset")
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "url", Value: 1}}},
		{Keys: bson.D{{Key: "ip", Value: 1}}},
		{Keys: bson.D{{Key: "port", Value: 1}}},
		{Keys: bson.D{{Key: "host", Value: 1}, {Key: "port", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "project", Value: 1}, {Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "rootDomain", Value: 1}}},
		// 1.8 新增索引
		{Keys: bson.D{{Key: "tags", Value: 1}}},
		// HTTP 状态视图：按状态码档位 + 时间倒序
		{Keys: bson.D{{Key: "statuscode", Value: 1}, {Key: "time", Value: -1}}},
		// 服务维度：按 service 分组
		{Keys: bson.D{{Key: "service", Value: 1}, {Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "project", Value: 1}, {Key: "statuscode", Value: 1}}},
		{Keys: bson.D{{Key: "technologies", Value: 1}}},
		{Keys: bson.D{{Key: "faviconmmh3", Value: 1}}},
		{Keys: bson.D{{Key: "bodyhash", Value: 1}}},
		// 1.8.1新增索引
		{Keys: bson.D{{Key: "host", Value: 1}}},

		{Keys: bson.D{{Key: "rootDomain", Value: 1}, {Key: "time", Value: -1}}}, // 优化按 rootDomain 查询并按 time 排序的性能

		// 指纹统计读模型（对标 ARL-Next assetStatFinger）：
		// technologies 是多值字段，{technologies:1} 单字段 multikey 索引上面已有；
		// 这里补两条：
		//   - {technologies, time}: 按技术反查资产并按 time 倒序，索引直接提供排序，不退化成 blocking sort；
		//   - {project, technologies}: 反查时的 count(total) 走 COUNT_SCAN（只读索引不回表，explain 实测）。
		{Keys: bson.D{{Key: "technologies", Value: 1}, {Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "project", Value: 1}, {Key: "technologies", Value: 1}}},

		// 证书读模型（对标 ARL-Next assetCert）：
		// 扫描端把 tlsx 证书写在 tls.certificateresponse.*（无 bson tag 的嵌入指针结构体，
		// 驱动编码成 certificateresponse 子文档）；服务端 legacy 模型是 tlsdata.*。
		// 这些字段用于详情定位与按到期时间取数，sparse 避免给无证书资产建空键。
		//   - fingerprint.sha256: 证书详情按指纹回到 TLS 文档；
		//   - notafter: 按到期时间范围取数（当前列表的过期过滤在分组后做，
		//     保证同一证书的 host 计数完整；索引为日期范围查询预留）；
		//   - subjectan: 证书 SAN 是多值字段，multikey 索引，支持按 SAN 反查（预留）；
		//   - tlsdata.not_after: legacy map 形状的日期字段。
		{Keys: bson.D{{Key: "tls.certificateresponse.fingerprinthash.sha256", Value: 1}}, Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "tls.certificateresponse.notafter", Value: 1}}, Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "tls.certificateresponse.subjectan", Value: 1}}, Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "tlsdata.not_after", Value: 1}}, Options: options.Index().SetSparse(true)},
	}
	_, err = asset.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create asset indexes: %v", err)
	}

	// 创建ip资产临时集合索引
	ipAssetTmp := db.Collection("IPAssetTmp")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "ip", Value: 1}, {Key: "port", Value: 1}, {Key: "domain", Value: 1}}, Options: options.Index().SetUnique(true)},
	}
	_, err = ipAssetTmp.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create ipAssetTmp indexes: %v", err)
	}

	// 创建ip资产集合索引
	ipAsset := db.Collection("IPAsset")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "ports.server.domain", Value: 1}}},
		{Keys: bson.D{{Key: "ports.port", Value: 1}}},
		{Keys: bson.D{{Key: "ip", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "ports.server.service", Value: 1}}},
		{Keys: bson.D{{Key: "ports.server.webServer", Value: 1}}},
		{Keys: bson.D{{Key: "rootDomain", Value: 1}}},
		{Keys: bson.D{{Key: "ports.server.technologies", Value: 1}}},
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
	}
	_, err = ipAsset.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create ipAsset indexes: %v", err)
	}

	// 创建icon集合索引
	icon := db.Collection("icon")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "fav3", Value: 1}}, Options: options.Index().SetUnique(true)},
	}
	_, err = icon.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create icon indexes: %v", err)
	}

	// 创建http res body集合索引
	httpBody := db.Collection("HttpBody")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "hash", Value: 1}}, Options: options.Index().SetUnique(true)},
	}
	_, err = httpBody.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create httpBody indexes: %v", err)
	}

	// 创建http截图索引
	httpScreenshot := db.Collection("screenshot")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "hash", Value: 1}}, Options: options.Index().SetUnique(true)},
	}
	_, err = httpScreenshot.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create httpBody indexes: %v", err)
	}

	// 创建子域名索引
	subdomain := db.Collection("subdomain")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "rootDomain", Value: 1}}},
		{Keys: bson.D{{Key: "time", Value: 1}}},
		// site 页的「子域链路」按 host 精确查 + 时间倒序：没有这个索引会全表扫
		{Keys: bson.D{{Key: "host", Value: 1}, {Key: "time", Value: -1}}},
	}
	_, err = subdomain.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create subdomain indexes: %v", err)
	}

	// 创建URL扫描索引
	urlScan := db.Collection("UrlScan")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "rootDomain", Value: 1}}},
		// 列表按 time 倒序分页；没有这个索引时是对全集合 SORT（实测 20 万条约 29ms 且吃内存）
		{Keys: bson.D{{Key: "time", Value: -1}}},
	}
	_, err = urlScan.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create UrlScan indexes: %v", err)
	}

	// 创建敏感信息body索引
	SensitiveBody := db.Collection("SensitiveBody")
	_, err = SensitiveBody.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "md5", Value: 1}},
	})
	if err != nil {
		log.Printf("failed to create SensitiveBody index: %v", err)
	}
	// 创建爬虫索引
	crawler := db.Collection("crawler")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "rootDomain", Value: 1}}},
		{Keys: bson.D{{Key: "time", Value: -1}}},
	}
	_, err = crawler.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create crawler indexes: %v", err)
	}

	// 创建敏感信息结果索引
	sensitiveResult := db.Collection("SensitiveResult")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "rootDomain", Value: 1}}},
		{Keys: bson.D{{Key: "time", Value: -1}}},
	}
	_, err = sensitiveResult.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create SensitiveResult indexes: %v", err)
	}
	// WIH 敏感信息读模型（对标 ARL-Next assetWih）：type(->sid)/project 过滤 + time 倒序分页
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}, {Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "sid", Value: 1}, {Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "project", Value: 1}, {Key: "sid", Value: 1}, {Key: "time", Value: -1}}},
	}
	_, err = sensitiveResult.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create SensitiveResult read model indexes: %v", err)
	}

	// 创建目录扫描结果索引
	dirScanResult := db.Collection("DirScanResult")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "rootDomain", Value: 1}}},
		{Keys: bson.D{{Key: "time", Value: -1}}},
	}
	_, err = dirScanResult.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create DirScanResult indexes: %v", err)
	}

	// 创建漏洞索引
	vulnerability := db.Collection("vulnerability")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "rootDomain", Value: 1}}},
		// 扫描侧 bson 编码落的是小写 rootdomain（types.VulnResult 只打了 yaml tag），
		// 按根域名过滤/导出要能吃到新数据，索引得建在实际写入的键上。
		{Keys: bson.D{{Key: "rootdomain", Value: 1}}},
		{Keys: bson.D{{Key: "hash", Value: 1}}},
		// 列表按 time 倒序分页：实测 20 万条从 29ms 降到 3ms，且避免大集合的 blocking sort
		{Keys: bson.D{{Key: "time", Value: -1}}},
	}
	_, err = vulnerability.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create vulnerability indexes: %v", err)
	}
	// Nuclei 结果读模型（对标 ARL-Next assetNucleiResult）：
	// 过滤 project/severity 后按 time 倒序分页，三种组合各配一条索引，
	// 让 count 与列表的排序都落在索引上（explain 实测三种形状都是 IXSCAN + 无 blocking sort）。
	// templateId 是子串匹配，正则没有索引边界，故意不为 vulnid 建「用不上」的索引。
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}, {Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "level", Value: 1}, {Key: "time", Value: -1}}},
		{Keys: bson.D{{Key: "project", Value: 1}, {Key: "level", Value: 1}, {Key: "time", Value: -1}}},
		// 处置状态筛选与进度统计（v2.0.208/220 新增能力）：
		// {status:1} 让聚合分组不走 COLLSCAN；time 参与排序避免 blocking sort。
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "time", Value: -1}}},
	}
	_, err = vulnerability.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create vulnerability read model indexes: %v", err)
	}

	// 创建vulDetail索引
	vulnerabilityDetail := db.Collection("vulnerabilityDetail")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "hash", Value: 1}}},
	}
	_, err = vulnerabilityDetail.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create vulnerabilityDetail indexes: %v", err)
	}

	// 创建根域名索引
	rootDomain := db.Collection("RootDomain")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "domain", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "time", Value: 1}}},
		// ICP 主体读模型（对标 ARL-Next assetCip）：按 company 分组统计域名数，
		// 详情按 company 过滤并按 time 倒序分页；复合索引直接提供过滤 + 排序。
		{Keys: bson.D{{Key: "company", Value: 1}, {Key: "time", Value: -1}}},
	}
	_, err = rootDomain.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create RootDomain indexes: %v", err)
	}

	// 创建应用索引
	app := db.Collection("app")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "time", Value: 1}}},
		{Keys: bson.D{{Key: "name", Value: 1}}},
	}
	_, err = app.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create app indexes: %v", err)
	}

	// 创建MP索引
	mp := db.Collection("mp")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "project", Value: 1}}},
		{Keys: bson.D{{Key: "taskName", Value: 1}}},
		{Keys: bson.D{{Key: "time", Value: 1}}},
		{Keys: bson.D{{Key: "name", Value: 1}}},
	}
	_, err = mp.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create mp indexes: %v", err)
	}

	// 创建task相关索引
	task := db.Collection("task")
	indexes = []mongo.IndexModel{
		{Keys: bson.D{{Key: "creatTime", Value: -1}}},
		{Keys: bson.D{{Key: "name", Value: 1}}},
		{Keys: bson.D{{Key: "progress", Value: 1}}},
	}
	_, err = task.Indexes().CreateMany(context.Background(), indexes)
	if err != nil {
		return fmt.Errorf("failed to create task indexes: %v", err)
	}

	// 仪表盘 / 控制台读模型（对标 ARL-Next dashboard.py + console.py）：
	//   - 趋势的「当天完成任务数」= status=3 且 endTime 落在窗口内，
	//     {status,endTime} 让 $match 整段走索引（status 前缀也能单独服务「运行中/已完成」计数）；
	//   - 统计卡片的「任务」按 project 过滤（task.project 是数组，multikey）。
	// 资产 / 漏洞 / URL / 子域 / 敏感信息的 time 与 project 索引上面已经建过（asset/vulnerability/
	// UrlScan/subdomain/SensitiveResult 都有 project 与 time 相关索引），这里不重复创建。
	_, err = task.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "endTime", Value: -1}}},
		{Keys: bson.D{{Key: "endTime", Value: -1}}},
		{Keys: bson.D{{Key: "project", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("failed to create task dashboard indexes: %v", err)
	}

	// 资产集团（对标 ARL-Next asset_group）：名称唯一
	assetGroup := db.Collection("asset_group")
	_, err = assetGroup.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "name", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "sort_order", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("failed to create asset_group indexes: %v", err)
	}

	// MCP 调用审计（对标 ARL-Next mcp_call_logs）：
	// 列表按 createdAt 倒序分页、按 keyId 过滤后仍按时间排序、按 tool 聚合统计
	mcpCallLogs := db.Collection("mcp_call_logs")
	if _, err := mcpCallLogs.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "createdAt", Value: -1}}},
		{Keys: bson.D{{Key: "keyId", Value: 1}, {Key: "createdAt", Value: -1}}},
		{Keys: bson.D{{Key: "tool", Value: 1}}},
		// 审计日志保留 30 天：每分钟都在写，不设 TTL 会无限增长
		{
			Keys:    bson.D{{Key: "createdAt", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(30 * 24 * 3600).SetName("createdAt_ttl30d"),
		},
	}); err != nil {
		return fmt.Errorf("failed to create mcp_call_logs indexes: %v", err)
	}

	// 调度拒绝记录保留 30 天（每次撞闸一条，长期会无限增长）
	rejections := db.Collection("dispatch_rejections")
	if _, err := rejections.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(30 * 24 * 3600).SetName("at_ttl30d"),
	}); err != nil {
		return fmt.Errorf("failed to create dispatch_rejections ttl index: %v", err)
	}

	// 操作审计同样保留 30 天
	auditLogs := db.Collection("audit_logs")
	if _, err := auditLogs.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(30 * 24 * 3600).SetName("at_ttl30d"),
	}); err != nil {
		return fmt.Errorf("failed to create audit_logs ttl index: %v", err)
	}
	// 这里不重复创建，避免历史库里存在重复 keyHash 时启动直接失败）
	apiKeyCollection := db.Collection("api_key")
	if _, err := apiKeyCollection.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "createdAt", Value: -1}},
	}); err != nil {
		return fmt.Errorf("failed to create api_key createdAt index: %v", err)
	}

	// 用户管理（对标 ARL-Next user.py）：列表分页 / 角色计数 / 用户名邮箱唯一
	if err := createUserIndexes(context.Background(), db); err != nil {
		return err
	}

	// 资产组地契（对标 ARL-Next asset_scope）：按集团/名称查询
	assetScope := db.Collection("asset_scope")
	_, err = assetScope.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "group_id", Value: 1}}},
		{Keys: bson.D{{Key: "name", Value: 1}}},
		{Keys: bson.D{{Key: "created_at", Value: -1}}},
	})
	if err != nil {
		return fmt.Errorf("failed to create asset_scope indexes: %v", err)
	}

	// ICP 企业主体 ↔ 域名查询（对标 ARL-Next icp_task / icp_asset）：
	//   - icp_task(state,createdAt)：列表按状态过滤后倒序 + overview 按 state 计数；
	//   - icp_result(taskId,createdAt)：任务结果分页 / CSV 导出；
	//   - icp_result(domain) / (company)：manual provider 按域名/企业回读本地记录；
	//   - icp_result(domain,company,icpNo) 唯一：跨任务去重键（bulk upsert 落点）；
	//   - icp_task_log(taskId,createdAt)：log 端点取最近 200 行。
	icpTask := db.Collection("icp_task")
	if _, err := icpTask.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "state", Value: 1}, {Key: "createdAt", Value: -1}},
		Options: options.Index().SetName("icp_task_state_created"),
	}); err != nil {
		return fmt.Errorf("failed to create icp_task indexes: %v", err)
	}
	icpResult := db.Collection("icp_result")
	if _, err := icpResult.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "taskId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("icp_result_task_created"),
		},
		{
			Keys:    bson.D{{Key: "domain", Value: 1}},
			Options: options.Index().SetName("icp_result_domain"),
		},
		{
			Keys:    bson.D{{Key: "company", Value: 1}},
			Options: options.Index().SetName("icp_result_company"),
		},
	}); err != nil {
		return fmt.Errorf("failed to create icp_result indexes: %v", err)
	}
	// 去重键：跨任务唯一。历史库（手工导入）若已有重复，唯一索引建不起来，
	// 退化成普通索引，避免脏数据把启动直接卡死；业务侧 bulk upsert 仍按同一组键去重。
	if _, err := icpResult.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "domain", Value: 1}, {Key: "company", Value: 1}, {Key: "icpNo", Value: 1}},
		Options: options.Index().SetName("icp_result_dedup").SetUnique(true),
	}); err != nil {
		if _, fallbackErr := icpResult.Indexes().CreateOne(context.Background(), mongo.IndexModel{
			Keys: bson.D{{Key: "domain", Value: 1}, {Key: "company", Value: 1}, {Key: "icpNo", Value: 1}},
		}); fallbackErr != nil {
			return fmt.Errorf("failed to create icp_result dedup index: %v", fallbackErr)
		}
		logger.Warn(fmt.Sprintf("icp_result unique index unavailable, fallback to plain index: %v", err))
	}
	icpTaskLog := db.Collection("icp_task_log")
	if _, err := icpTaskLog.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "taskId", Value: 1}, {Key: "createdAt", Value: -1}},
		Options: options.Index().SetName("icp_task_log_task_created"),
	}); err != nil {
		return fmt.Errorf("failed to create icp_task_log indexes: %v", err)
	}

	// FOFA 批量建任务（对标 ARL-Next taskFofa.py）：
	//   - fofa_import(query,createdAt)：同 query 取最近一次导入做 host 去重（query 等值 + createdAt 倒序取 1）；
	//   - fofa_import.createdAt：导入历史倒序分页。
	fofaImport := db.Collection("fofa_import")
	if _, err := fofaImport.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "query", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("fofa_import_query_created"),
		},
		{
			Keys:    bson.D{{Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("fofa_import_created"),
		},
	}); err != nil {
		return fmt.Errorf("failed to create fofa_import indexes: %v", err)
	}

	// 字典元数据（对标 ARL-Next dictionary.py）：列表按 kind 过滤 + updated_at 倒序；
	// name 不做唯一索引——历史库可能已有重名，唯一索引会让启动直接失败。
	dictionary := db.Collection("dictionary")
	_, err = dictionary.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "category", Value: 1}, {Key: "name", Value: 1}}},
		{Keys: bson.D{{Key: "category", Value: 1}, {Key: "updated_at", Value: -1}}},
		{Keys: bson.D{{Key: "updated_at", Value: -1}}},
		{Keys: bson.D{{Key: "name", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("failed to create dictionary indexes: %v", err)
	}

	// 字典行级存储：(dict_id, value) 唯一是「追加去重」的核心，
	// (dict_id, seq) 支撑按写入顺序做预览/导出。
	dictionaryEntry := db.Collection("dictionary_entry")
	_, err = dictionaryEntry.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "dict_id", Value: 1}, {Key: "value", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		// 唯一索引建不起来（历史脏数据）时退化为普通索引：去重仍有业务兜底，但不会阻塞启动
		if _, fallbackErr := dictionaryEntry.Indexes().CreateOne(context.Background(), mongo.IndexModel{
			Keys: bson.D{{Key: "dict_id", Value: 1}, {Key: "value", Value: 1}},
		}); fallbackErr != nil {
			return fmt.Errorf("failed to create dictionary_entry value index: %v", fallbackErr)
		}
		logger.Warn(fmt.Sprintf("dictionary_entry unique index unavailable, fallback to plain index: %v", err))
	}
	_, err = dictionaryEntry.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "dict_id", Value: 1}, {Key: "seq", Value: 1}}},
		{Keys: bson.D{{Key: "category", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("failed to create dictionary_entry indexes: %v", err)
	}

	// GridFS 内容元数据：字典下载/删除都按 filename 查（内容文件以字典 ID 命名）。
	// driver 只在 fs.files 为空时自动补同名索引，升级库可能已经错过，这里显式创建。
	filesColl := db.Collection("fs.files")
	_, err = filesColl.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "filename", Value: 1}, {Key: "uploadDate", Value: 1}},
	})
	if err != nil {
		return fmt.Errorf("failed to create fs.files filename index: %v", err)
	}

	// 大文件上传任务：task_id 唯一，status 用于重启恢复扫描
	uploadTasks := db.Collection("dict_upload_tasks")
	_, err = uploadTasks.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "task_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "created_at", Value: -1}}},
	})
	if err != nil {
		return fmt.Errorf("failed to create dict_upload_tasks indexes: %v", err)
	}

	// 批量导出（/api/export/batch、/api/export/task/:taskId）的流式读索引。
	//
	// 导出的查询形状固定是「filter(project 或 taskName) + sort(time 倒序) + cursor 流式写」：
	// 缺 {project,time:-1} / {taskName,time:-1} 时，Mongo 要么退化成 COLLSCAN，
	// 要么为了排序起一个阻塞式 SORT——find 的排序内存上限只有 32MB，
	// 大集合上会直接 "Sort exceeded memory limit"，导出就废了。
	// 只补还没有的组合：asset / vulnerability / SensitiveResult 的 {project,time:-1}
	// 已由前面的读模型索引提供；相同键的索引重复创建是 no-op，所以不指定 name。
	exportIndexes := []struct {
		collection string
		keys       []bson.D
	}{
		{"asset", []bson.D{
			{{Key: "taskName", Value: 1}, {Key: "time", Value: -1}},
		}},
		{"subdomain", []bson.D{
			{{Key: "project", Value: 1}, {Key: "time", Value: -1}},
			{{Key: "taskName", Value: 1}, {Key: "time", Value: -1}},
		}},
		{"UrlScan", []bson.D{
			{{Key: "project", Value: 1}, {Key: "time", Value: -1}},
			{{Key: "taskName", Value: 1}, {Key: "time", Value: -1}},
			// status 过滤 + time 倒序：资产页/MCP filter 的热路径（UrlScan 的字段名是
			// status 而非 statuscode；filter 别名见 helper.normalizeFilterValue）
			{{Key: "status", Value: 1}, {Key: "time", Value: -1}},
		}},
		{"IPAsset", []bson.D{
			{{Key: "project", Value: 1}, {Key: "time", Value: -1}},
			{{Key: "taskName", Value: 1}, {Key: "time", Value: -1}},
		}},
		{"RootDomain", []bson.D{
			{{Key: "project", Value: 1}, {Key: "time", Value: -1}},
			{{Key: "taskName", Value: 1}, {Key: "time", Value: -1}},
		}},
		{"SensitiveResult", []bson.D{
			{{Key: "taskName", Value: 1}, {Key: "time", Value: -1}},
		}},
		{"vulnerability", []bson.D{
			{{Key: "taskName", Value: 1}, {Key: "time", Value: -1}},
		}},
	}
	for _, group := range exportIndexes {
		idxModels := make([]mongo.IndexModel, 0, len(group.keys))
		for _, keys := range group.keys {
			idxModels = append(idxModels, mongo.IndexModel{Keys: keys})
		}
		if _, err := db.Collection(group.collection).Indexes().CreateMany(context.Background(), idxModels); err != nil {
			return fmt.Errorf("failed to create %s export indexes: %v", group.collection, err)
		}
	}

	// 扫描策略（对标 ARL-Next policy.py）：
	//   - name 唯一：同名的第二份策略在建/改接口里就被拒（400），索引是并发兜底；
	//   - enabled+updatedAt：列表按 enabled 过滤后仍按更新时间倒序，索引直接提供过滤 + 排序；
	//   - updatedAt：不带 enabled 过滤的默认列表（新→旧），避免大集合上退化成内存排序。
	scanPolicy := db.Collection("scan_policy")
	if _, err := scanPolicy.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "name", Value: 1}},
			Options: options.Index().SetName("scan_policy_name_unique").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "enabled", Value: 1}, {Key: "updatedAt", Value: -1}},
			Options: options.Index().SetName("scan_policy_enabled_updated"),
		},
		{
			Keys:    bson.D{{Key: "updatedAt", Value: -1}},
			Options: options.Index().SetName("scan_policy_updated"),
		},
	}); err != nil {
		return fmt.Errorf("failed to create scan_policy indexes: %v", err)
	}

	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
