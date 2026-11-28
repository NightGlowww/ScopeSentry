package node

import "testing"

// 上下线通知的口径：只在「变化」时通知，首次观测不通知（否则每次发布都会给所有节点发一遍）。
// 这一层是纯函数，服务里只负责把存活判定结果喂进来。
func TestDiffLiveness(t *testing.T) {
	prev := map[string]bool{"n1": true, "n2": true, "n3": false}
	cur := map[string]bool{
		"n1": false, // 掉线
		"n2": true,  // 保持在线
		"n3": true,  // 恢复
		"n4": true,  // 首次观测
	}
	down, up := diffLiveness(prev, cur)
	if len(down) != 1 || down[0] != "n1" {
		t.Fatalf("掉线列表不对: %v", down)
	}
	if len(up) != 1 || up[0] != "n3" {
		t.Fatalf("恢复列表不对: %v", up)
	}
	// 首次观测的 n4 不能出现在恢复列表里
	for _, n := range up {
		if n == "n4" {
			t.Fatalf("首次观测不应通知: %v", up)
		}
	}

	// 没有任何变化 → 两个列表都空（这是「不要每次轮询都发」的核心保证）
	d2, u2 := diffLiveness(cur, cur)
	if len(d2) != 0 || len(u2) != 0 {
		t.Fatalf("状态未变时不应有通知: down=%v up=%v", d2, u2)
	}
}
