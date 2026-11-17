// repositories/backup-------------------------------------
// @file      : store.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 01:12
// -------------------------------------------

package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

var (
	// ErrInvalidBackupName 备份文件名非法（空、含路径、非 tar.gz）
	ErrInvalidBackupName = errors.New("invalid backup name")
	// ErrBackupNotFound 备份包不存在
	ErrBackupNotFound = errors.New("backup not found")
	// ErrBackupExists 同名备份包已存在
	ErrBackupExists = errors.New("backup already exists")
)

// backupNameRE 只允许备份目录内的平面文件名，且必须以 .tar.gz 结尾。
// 这个正则本身就是路径穿越的第一道闸：任何斜杠、反斜杠、绝对路径都不能通过。
var backupNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.tar\.gz$`)

const (
	backupNameMaxLen = 160
	metaSuffix       = ".meta.json"
)

// ValidateBackupName 校验备份文件名，拒绝路径穿越与非法字符。
func ValidateBackupName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty name", ErrInvalidBackupName)
	}
	if len(name) > backupNameMaxLen {
		return fmt.Errorf("%w: name too long", ErrInvalidBackupName)
	}
	if strings.ContainsAny(name, "/\\") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: %q contains path separator", ErrInvalidBackupName, name)
	}
	if name != filepath.Base(name) || strings.Contains(name, "..") {
		return fmt.Errorf("%w: %q is not a plain file name", ErrInvalidBackupName, name)
	}
	if !backupNameRE.MatchString(name) {
		return fmt.Errorf("%w: %q must be a *.tar.gz backup file", ErrInvalidBackupName, name)
	}
	return nil
}

// Store 备份目录的落盘操作。备份包与同名 *.meta.json 旁车文件都放在 Dir() 下。
type Store interface {
	Dir() string
	Ensure() error
	Path(name string) (string, error)
	Exists(name string) (bool, error)
	List() ([]models.BackupInfo, error)
	Delete(name string) error
	SaveMeta(info models.BackupInfo) error
	UniqueName(stamp string) (string, error)
	HashFile(path string) (string, error)
}

type store struct {
	dir string
}

// NewStore 创建备份目录 store
func NewStore(dir string) Store {
	return &store{dir: dir}
}

func (s *store) Dir() string { return s.dir }

func (s *store) Ensure() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create backup dir %s: %w", s.dir, err)
	}
	return nil
}

// Path 校验文件名并返回落盘路径。
func (s *store) Path(name string) (string, error) {
	if err := ValidateBackupName(name); err != nil {
		return "", err
	}
	full := filepath.Join(s.dir, name)
	// 双保险：Join 之后必须仍然直接位于备份目录下。
	if filepath.Dir(full) != filepath.Clean(s.dir) {
		return "", fmt.Errorf("%w: %q escapes backup dir", ErrInvalidBackupName, name)
	}
	return full, nil
}

func (s *store) Exists(name string) (bool, error) {
	full, err := s.Path(name)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

// List 列出备份目录内的备份包，按创建时间倒序。
func (s *store) List() ([]models.BackupInfo, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.BackupInfo{}, nil
		}
		return nil, fmt.Errorf("read backup dir: %w", err)
	}

	infos := make([]models.BackupInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if ValidateBackupName(name) != nil {
			continue
		}
		fileInfo, err := entry.Info()
		if err != nil || !fileInfo.Mode().IsRegular() {
			continue
		}
		info, err := s.readMeta(name)
		if err != nil {
			// 没有旁车文件（例如人工拷进来的包）：现场计算，保证 list 的 sha256 可用。
			info = models.BackupInfo{
				Name:      name,
				CreatedAt: fileInfo.ModTime().Format(time.RFC3339),
			}
			if full, pathErr := s.Path(name); pathErr == nil {
				if sum, hashErr := s.HashFile(full); hashErr == nil {
					info.SHA256 = sum
				}
			}
		}
		info.Name = name
		info.Size = fileInfo.Size()
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		if infos[i].CreatedAt != infos[j].CreatedAt {
			return infos[i].CreatedAt > infos[j].CreatedAt
		}
		return infos[i].Name > infos[j].Name
	})
	return infos, nil
}

func (s *store) Delete(name string) error {
	full, err := s.Path(name)
	if err != nil {
		return err
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrBackupNotFound, name)
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrBackupNotFound, name)
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("remove backup %s: %w", name, err)
	}
	if err := os.Remove(full + metaSuffix); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove backup meta %s: %w", name, err)
	}
	return nil
}

// SaveMeta 原子写入旁车元数据（note/sha256 无法从 tar 文件名得到）。
func (s *store) SaveMeta(info models.BackupInfo) error {
	if err := ValidateBackupName(info.Name); err != nil {
		return err
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal backup meta: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".meta-*")
	if err != nil {
		return fmt.Errorf("create meta tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write meta: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close meta: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(s.dir, info.Name+metaSuffix)); err != nil {
		return fmt.Errorf("rename meta: %w", err)
	}
	return nil
}

func (s *store) readMeta(name string) (models.BackupInfo, error) {
	var info models.BackupInfo
	data, err := os.ReadFile(filepath.Join(s.dir, name+metaSuffix))
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return info, err
	}
	return info, nil
}

// UniqueName 生成不冲突的备份文件名；同一秒内多次备份会追加 -2/-3 后缀。
func (s *store) UniqueName(stamp string) (string, error) {
	if stamp == "" {
		return "", fmt.Errorf("empty timestamp for backup name")
	}
	name := fmt.Sprintf("backup-%s.tar.gz", stamp)
	for i := 2; i < 10000; i++ {
		full, err := s.Path(name)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(full); err != nil {
			if os.IsNotExist(err) {
				return name, nil
			}
			return "", err
		}
		name = fmt.Sprintf("backup-%s-%d.tar.gz", stamp, i)
	}
	return "", fmt.Errorf("too many backups for timestamp %s", stamp)
}

// HashFile 计算文件的 sha256（小写 hex）。
func (s *store) HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
