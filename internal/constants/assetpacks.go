// Package constants -----------------------------
// @file      : assetpacks.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 23:55
// -------------------------------------------
package constants

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 资产包（字典 / nuclei 模板）由外部转换器产出，经 tools/assetpack/import.py 校验后落盘。
//
// 这一层只认「包」这个格式，不认具体来源：manifest 里 source 只是可选的溯源元数据，
// 换字典源或再挂一套 nuclei 模板，只要产物落到 assets/dict 与 assets/poc 就能被吃进来。
// 产品本身不依赖任何外部仓库：assets 是唯一输入。
//
//go:embed assets/dict
var dictPackFS embed.FS

//go:embed assets/poc
var pocPackFS embed.FS

// DictPack 一个字典包的元数据（assets/dict/manifest.json 的 packs[] 元素）
type DictPack struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Embedded     bool     `json:"embedded"`
	File         string   `json:"file"`
	Lines        int      `json:"lines"`
	Bytes        int      `json:"bytes"`
	SHA256       string   `json:"sha256"`
	SkippedLines int      `json:"skipped_lines"`
	Sources      []string `json:"sources"`
}

// DictPackManifest 字典包清单
type DictPackManifest struct {
	Schema    int    `json:"schema"`
	Generator string `json:"generator"`
	// Source 可选溯源信息（来源名/版本），仅用于排查，不参与任何运行逻辑
	Source map[string]string `json:"source,omitempty"`
	Packs  []DictPack        `json:"packs"`
}

// POCPack 一个随二进制分发的 nuclei 模板
type POCPack struct {
	ID       string
	FileName string
	Content  string
}

// AssetPackManifest 读取内嵌的字典包清单
func AssetPackManifest() (*DictPackManifest, error) {
	data, err := dictPackFS.ReadFile("assets/dict/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("read dict pack manifest: %w", err)
	}
	var manifest DictPackManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse dict pack manifest: %w", err)
	}
	return &manifest, nil
}

// EmbeddedDictPacks 返回随二进制分发的字典包（embedded=true），按 kind/name 排序。
// 非内嵌的大包只在 dist/packs 里，由运维手工导入，避免二进制膨胀。
func EmbeddedDictPacks() ([]DictPack, error) {
	manifest, err := AssetPackManifest()
	if err != nil {
		return nil, err
	}
	packs := make([]DictPack, 0, len(manifest.Packs))
	for _, pack := range manifest.Packs {
		if pack.Embedded {
			packs = append(packs, pack)
		}
	}
	sort.Slice(packs, func(i, j int) bool {
		if packs[i].Kind == packs[j].Kind {
			return packs[i].Name < packs[j].Name
		}
		return packs[i].Kind < packs[j].Kind
	})
	return packs, nil
}

// DictPackContent 读取内嵌字典包内容
func DictPackContent(pack DictPack) ([]byte, error) {
	name := pack.Name
	if !strings.HasSuffix(name, ".txt") {
		name += ".txt"
	}
	data, err := dictPackFS.ReadFile(path.Join("assets/dict", pack.Kind, name))
	if err != nil {
		return nil, fmt.Errorf("read dict pack %s/%s: %w", pack.Kind, pack.Name, err)
	}
	return data, nil
}

// POCPacks 返回内嵌的 nuclei 模板（文件名排序，保证入库顺序稳定）
func POCPacks() ([]POCPack, error) {
	entries, err := fs.ReadDir(pocPackFS, "assets/poc")
	if err != nil {
		return nil, fmt.Errorf("read poc pack dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	packs := make([]POCPack, 0, len(names))
	for _, name := range names {
		content, err := pocPackFS.ReadFile(path.Join("assets/poc", name))
		if err != nil {
			return nil, fmt.Errorf("read poc %s: %w", name, err)
		}
		var header models.PocTemplate
		if err := yaml.Unmarshal(content, &header); err != nil {
			return nil, fmt.Errorf("parse poc %s: %w", name, err)
		}
		if header.ID == "" || header.Info.Name == "" {
			return nil, fmt.Errorf("poc %s missing id/info.name", name)
		}
		packs = append(packs, POCPack{ID: header.ID, FileName: name, Content: string(content)})
	}
	return packs, nil
}

// AssetPackDigest 内嵌资产包的内容指纹（不含时间/来源等易变字段）。
//
// 播种拿它当幂等版本号：包内容变了指纹就变，下次启动自动补写；
// 不再需要人工维护 "xxx-1" 这类版本串，也就不会因为忘 bump 而漏升级，
// 更不会把某个具体上游的名字写进产品常量里。
func AssetPackDigest() (string, error) {
	packs, err := EmbeddedDictPacks()
	if err != nil {
		return "", err
	}
	templates, err := POCPacks()
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	for _, pack := range packs {
		fmt.Fprintf(digest, "dict/%s/%s/%s\n", pack.Kind, pack.Name, pack.SHA256)
	}
	for _, template := range templates {
		sum := sha256.Sum256([]byte(template.Content))
		fmt.Fprintf(digest, "poc/%s/%x\n", template.ID, sum)
	}
	return hex.EncodeToString(digest.Sum(nil))[:16], nil
}
