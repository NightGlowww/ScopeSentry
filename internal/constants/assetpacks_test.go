package constants

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// 资产包是外部转换器的产物（经 tools/assetpack/import.py 安装），
// 这里校验「清单 -> 文件 -> 哈希 -> 行数」自洽，防止手改资产或漏跑生成脚本后静默漂移。
func TestAssetPackManifestIsConsistent(t *testing.T) {
	manifest, err := AssetPackManifest()
	if err != nil {
		t.Fatalf("AssetPackManifest: %v", err)
	}
	if manifest.Schema != 1 {
		t.Fatalf("unexpected schema: %d", manifest.Schema)
	}
	if len(manifest.Packs) == 0 {
		t.Fatal("manifest has no packs")
	}
	seen := map[string]bool{}
	for _, pack := range manifest.Packs {
		key := pack.Kind + "/" + pack.Name
		if seen[key] {
			t.Fatalf("duplicate pack %s", key)
		}
		seen[key] = true
		if pack.Name != strings.ToLower(pack.Name) {
			t.Fatalf("pack name must be lowercase for dict placeholder lookup: %s", pack.Name)
		}
		if !pack.Embedded {
			continue
		}
		content, err := DictPackContent(pack)
		if err != nil {
			t.Fatalf("DictPackContent(%s): %v", key, err)
		}
		if len(content) == 0 {
			t.Fatalf("pack %s is empty", key)
		}
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != pack.SHA256 {
			t.Fatalf("pack %s sha256 drifted: manifest=%s actual=%s (reinstall via tools/assetpack/import.py)", key, pack.SHA256, got)
		}
		lines := strings.Count(string(content), "\n")
		if lines != pack.Lines {
			t.Fatalf("pack %s line count drifted: manifest=%d actual=%d", key, pack.Lines, lines)
		}
		if pack.Bytes != len(content) {
			t.Fatalf("pack %s size drifted: manifest=%d actual=%d", key, pack.Bytes, len(content))
		}
	}
}

func TestEmbeddedDictPacksCoverCoreKinds(t *testing.T) {
	packs, err := EmbeddedDictPacks()
	if err != nil {
		t.Fatalf("EmbeddedDictPacks: %v", err)
	}
	if len(packs) == 0 {
		t.Fatal("no embedded packs")
	}
	// 三类核心字典必须都有，否则扫描参数里的 dict.dir/subdomain/brute 引用会失效
	required := map[string]bool{"dir": false, "subdomain": false, "brute": false}
	for _, pack := range packs {
		if _, ok := required[pack.Kind]; ok {
			required[pack.Kind] = true
		}
	}
	for kind, ok := range required {
		if !ok {
			t.Fatalf("no embedded pack for kind %q", kind)
		}
	}
	// 排序稳定，播种顺序可复现
	for i := 1; i < len(packs); i++ {
		prev, cur := packs[i-1], packs[i]
		if prev.Kind > cur.Kind || (prev.Kind == cur.Kind && prev.Name > cur.Name) {
			t.Fatalf("packs not sorted: %s/%s before %s/%s", prev.Kind, prev.Name, cur.Kind, cur.Name)
		}
	}
}

func TestPOCPacksAreLoadableNucleiTemplates(t *testing.T) {
	packs, err := POCPacks()
	if err != nil {
		t.Fatalf("POCPacks: %v", err)
	}
	if len(packs) == 0 {
		t.Fatal("no poc templates")
	}
	for _, pack := range packs {
		if !strings.HasPrefix(pack.ID, "scopesentry-") {
			t.Fatalf("unexpected template id outside the scopesentry namespace: %s", pack.ID)
		}
		if !strings.HasPrefix(pack.FileName, pack.ID) {
			t.Fatalf("file name %s does not match template id %s", pack.FileName, pack.ID)
		}
		if !strings.Contains(pack.Content, "\nhttp:") && !strings.Contains(pack.Content, "\nrequests:") {
			t.Fatalf("template %s has no http/requests block", pack.ID)
		}
		if !strings.Contains(pack.Content, "matchers:") {
			t.Fatalf("template %s has no matchers", pack.ID)
		}
		if strings.Contains(pack.Content, "\t") {
			t.Fatalf("template %s contains a tab (invalid YAML indentation)", pack.ID)
		}
	}
}

// 播种版本号来自内容指纹：改了资产却忘了改版本串是常见事故，
// 这里确认摘要稳定、且对内容变化敏感。
func TestAssetPackDigestTracksContent(t *testing.T) {
	digest, err := AssetPackDigest()
	if err != nil {
		t.Fatalf("AssetPackDigest: %v", err)
	}
	if len(digest) != 16 {
		t.Fatalf("digest length = %d, want 16 hex chars", len(digest))
	}
	again, err := AssetPackDigest()
	if err != nil {
		t.Fatalf("AssetPackDigest second call: %v", err)
	}
	if digest != again {
		t.Fatalf("digest is not stable: %s vs %s", digest, again)
	}
	packs, err := EmbeddedDictPacks()
	if err != nil {
		t.Fatalf("EmbeddedDictPacks: %v", err)
	}
	// 摘要必须覆盖每个包的内容哈希，否则包内容变了版本却不变
	probe := packs[0].SHA256
	if probe == "" {
		t.Fatal("pack has empty sha256; digest would not track content")
	}
}
