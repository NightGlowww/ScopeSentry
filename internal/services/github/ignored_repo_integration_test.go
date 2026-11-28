//go:build integration

package github_test

// 黑名单仓库要真的生效：
//  1. 采集时命中黑名单的条目被跳过，且 summary.ignored 能看见；
//  2. purge=true 时把已入库的同仓库结果一起清掉。
// 这条用例存在的理由：ignoredRepos 曾经只存不用（接口能加、采集从不查），
// 「加了忽略但噪声照进」这种问题只有端到端跑一次才看得见。

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	githubsvc "github.com/Autumn-27/ScopeSentry/internal/services/github"
)

func TestGithubIgnoredRepoSkipsCollectionAndPurges(t *testing.T) {
	ctx := requireMongo(t)
	taskID := seedTask(t, ctx, models.GithubTypeCodeLeak, "it-ignored-repo")
	resultRepo := githubrepo.NewResultRepository()
	threatRepo := githubrepo.NewThreatRepository()

	// 先入库两条结果：一条来自将被忽略的仓库，一条正常仓库
	if _, err := resultRepo.UpsertHits(ctx, taskID, models.GithubTypeCodeLeak, "", models.GithubSourceTask, []githubrepo.Hit{
		{Repo: "acme/noisy-tutorials", Path: "a.env", Commit: "c1", Title: "noise", Snippet: "x=1"},
		{Repo: "acme/real-product", Path: "b.env", Commit: "c2", Title: "real", Snippet: "y=1"},
	}); err != nil {
		t.Fatalf("seed results: %v", err)
	}

	const ignoredRepo = "acme/noisy-tutorials"
	if _, err := threatRepo.AddIgnoredRepo(ctx, ignoredRepo, "it 用例"); err != nil {
		t.Fatalf("add ignored repo: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = threatRepo.RemoveIgnoredRepo(cleanup, ignoredRepo)
	})

	// 1) 采集入口应跳过该仓库：把搜索结果里的条目喂给 UpsertHits 前就该被过滤。
	//    这里直接验证过滤函数在服务内的效果 —— 用真实 RunTask 需要假 GitHub 服务器，
	//    已在 TestGithubRunDedupAndStats 覆盖链路；本用例聚焦「黑名单是否被读」这一点。
	patterns := githubsvc.IgnoredRepoPatternsForTest(githubsvc.NewService())
	if len(patterns) == 0 {
		t.Fatalf("黑名单没有被读到：ignoredRepos 只存不用会让人以为忽略了也没用")
	}
	if !githubsvc.MatchIgnoredRepoForTest(patterns, ignoredRepo) {
		t.Fatalf("黑名单命中判定失败：%v vs %s", patterns, ignoredRepo)
	}

	// 1b) 移除一个不在名单里的仓库必须回 false：
	//     只按 _id 过滤时 $set updatedAt 会让 ModifiedCount>0，于是「移除了不存在的条目」也报成功。
	if removed, err := threatRepo.RemoveIgnoredRepo(ctx, "it-nonexistent/repo"); err != nil {
		t.Fatalf("remove nonexistent: %v", err)
	} else if removed {
		t.Fatalf("移除不存在的仓库不应回 true（调用方会以为改对了）")
	}

	// 2) purge：删除已入库的同仓库结果，别的仓库不受影响
	removed, err := resultRepo.DeleteByRepo(ctx, ignoredRepo)
	if err != nil {
		t.Fatalf("delete by repo: %v", err)
	}
	if removed != 1 {
		t.Fatalf("应删除 1 条，实际 %d", removed)
	}
	if _, total, err := resultRepo.List(ctx, models.GithubResultListQuery{TaskID: taskID, PageIndex: 1, PageSize: 10}); err != nil || total != 1 {
		t.Fatalf("清理后应剩 1 条（正常仓库），实际 total=%d err=%v", total, err)
	}
}

// TestGithubRunTaskSkipsIgnoredRepo 端到端验证黑名单真的拦住了采集：
// 假 GitHub 返回 2 条（不同仓库），忽略其中一条 → 结果只剩 1 条且 summary.ignored=1。
// 这条比「过滤函数单测」硬：它证明 RunTask 确实读了配置，而不是写了个没人调的函数。
func TestGithubRunTaskSkipsIgnoredRepo(t *testing.T) {
	ctx := requireMongo(t)

	fake := &fakeGitHub{
		remaining: 29,
		reset:     time.Now().Add(time.Hour),
		items:     codeSearchItems(),
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	threatRepo := githubrepo.NewThreatRepository()
	const ignoredRepo = "acme/web-app"
	if _, err := threatRepo.AddIgnoredRepo(ctx, ignoredRepo, "it 端到端"); err != nil {
		t.Fatalf("add ignored repo: %v", err)
	}
	t.Cleanup(func() { _, _ = threatRepo.RemoveIgnoredRepo(context.Background(), ignoredRepo) })

	svc := githubsvc.NewService(githubsvc.WithBaseURL(srv.URL))
	taskID := seedTask(t, ctx, models.GithubTypeCodeLeak, "AWS_SECRET_ACCESS_KEY filename:.env")

	summary, err := svc.RunTask(ctx, taskID)
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if summary.Ignored != 1 {
		t.Fatalf("ignored = %d, want 1（黑名单没生效或被重复计数）", summary.Ignored)
	}
	if summary.Scanned != 1 || summary.NewFound != 1 {
		t.Fatalf("scanned/new = %d/%d, want 1/1", summary.Scanned, summary.NewFound)
	}

	results, total, err := githubrepo.NewResultRepository().List(ctx, models.GithubResultListQuery{TaskID: taskID, PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("结果数 = %d, want 1（被忽略的仓库不该入库）", total)
	}
	if results[0].Repo == ignoredRepo {
		t.Fatalf("入库的仍是被忽略的仓库: %s", results[0].Repo)
	}
}
