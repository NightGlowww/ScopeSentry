package user

import (
	"errors"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
)

// 纯函数校验：不碰数据库，覆盖角色 / 状态 / 密码长度 / 邮箱格式四条规则。
// 这些规则同时被 CreateUser、UpdateUser、ChangePassword 复用，改动会直接反映到 HTTP 400 上。

func TestValidateRole(t *testing.T) {
	for _, role := range []string{
		constants.RoleSuperAdmin,
		constants.RoleAdmin,
		constants.RoleUser,
		constants.RoleGuest,
	} {
		if err := ValidateRole(role); err != nil {
			t.Fatalf("ValidateRole(%q) = %v, want nil", role, err)
		}
	}

	for _, role := range []string{"", "  ", "root", "SUPER_ADMIN", "superadmin"} {
		err := ValidateRole(role)
		if !errors.Is(err, ErrInvalidRole) {
			t.Fatalf("ValidateRole(%q) = %v, want ErrInvalidRole", role, err)
		}
	}
}

func TestValidateStatus(t *testing.T) {
	for _, status := range []string{"active", "disabled"} {
		if err := ValidateStatus(status); err != nil {
			t.Fatalf("ValidateStatus(%q) = %v, want nil", status, err)
		}
	}
	for _, status := range []string{"", "disabled ", "banned", "ACTIVE"} {
		if err := ValidateStatus(status); !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("ValidateStatus(%q) = %v, want ErrInvalidStatus", status, err)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("12345678"); err != nil {
		t.Fatalf("8 位密码被拒: %v", err)
	}
	if err := ValidatePassword("密码密码密码密码"); err != nil {
		t.Fatalf("8 个中文（24 字节）被拒: %v", err)
	}
	if err := ValidatePassword("1234567"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("7 位密码 err = %v, want ErrWeakPassword", err)
	}
	if err := ValidatePassword("密码密码密码密"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("7 个中文 err = %v, want ErrWeakPassword", err)
	}
	if err := ValidatePassword(strings.Repeat("a", 257)); !errors.Is(err, ErrInvalidUserData) {
		t.Fatalf("超长密码 err = %v, want ErrInvalidUserData", err)
	}
}

func TestValidEmail(t *testing.T) {
	valid := []string{"a@b.com", "a@b", "user.name+tag@example.invalid"}
	for _, email := range valid {
		if !ValidEmail(email) {
			t.Fatalf("ValidEmail(%q) = false, want true", email)
		}
	}
	invalid := []string{"", "  ", "not-an-email", "a@@b.com", "a@b.com extra", "a b@c.com"}
	for _, email := range invalid {
		if ValidEmail(email) {
			t.Fatalf("ValidEmail(%q) = true, want false", email)
		}
	}
}

// 密码落库口径必须与登录校验一致：SHA256 十六进制（登录用同一个 helper 比对）。
func TestPasswordHashIsSha256Hex(t *testing.T) {
	if got := helper.Sha256Hex("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("Sha256Hex(abc) = %s", got)
	}
	first := helper.Sha256Hex("userpass123")
	if len(first) != 64 {
		t.Fatalf("哈希长度 = %d, want 64", len(first))
	}
	if first == "userpass123" {
		t.Fatal("密码被明文存储")
	}
	if first != helper.Sha256Hex("userpass123") {
		t.Fatal("密码哈希不稳定")
	}
}
