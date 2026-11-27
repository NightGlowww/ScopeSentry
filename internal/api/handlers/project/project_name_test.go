package project

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func postAdd(t *testing.T, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/project/add", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	AddProject(c)
	return w
}

// 空名字以前会真的建出一张没有标题的项目卡（models.Project 没有 binding 校验）。
// 现在必须在写库前挡住：{} 与纯空白都返回 400。
func TestAddProjectRejectsEmptyName(t *testing.T) {
	for _, body := range []string{`{}`, `{"name":""}`, `{"name":"   "}`} {
		w := postAdd(t, body)
		if w.Code != 400 {
			t.Fatalf("body %s => %d, want 400 (%s)", body, w.Code, w.Body.String())
		}
	}
}
