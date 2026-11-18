// services/backup-------------------------------------
// @file      : backup.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 01:22
// -------------------------------------------

package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	backupRepo "github.com/Autumn-27/ScopeSentry/internal/repositories/backup"
)

const (
	backupsDirName = "backups"
	// maxUploadSize 上传备份包的大小上限
	maxUploadSize int64 = 8 << 30
	// maxManifestSize manifest.json 的读取上限
	maxManifestSize int64 = 32 << 20
	// maxDocumentSize BSON 文档硬上限 16MB
	maxDocumentSize = 16 * 1024 * 1024
)

var (
	// ErrJobRunning 已有备份/恢复任务在跑
	ErrJobRunning = errors.New("another backup or restore task is running")
	// ErrConfirmRequired 恢复必须显式确认
	ErrConfirmRequired = errors.New("restore requires confirm field set to RESTORE")
	// ErrChecksumMismatch 上传包 sha256 与期望值不一致
	ErrChecksumMismatch = errors.New("backup sha256 mismatch")

	// 仓库层哨兵透出，handler 用 errors.Is 判断
	ErrInvalidName   = backupRepo.ErrInvalidBackupName
	ErrNotFound      = backupRepo.ErrBackupNotFound
	ErrAlreadyExists = backupRepo.ErrBackupExists
	ErrMongoDown     = backupRepo.ErrMongoUnavailable
)

// Service 全量备份 / 恢复
type Service interface {
	Create(ctx context.Context, note string) (*models.BackupInfo, error)
	List() ([]models.BackupInfo, error)
	Status() models.BackupStatus
	Path(name string) (string, error)
	Delete(name string) error
	Restore(ctx context.Context, name string) (*models.BackupRestoreResult, error)
	Upload(ctx context.Context, filename string, r io.Reader, expectedSHA256 string) (*models.BackupInfo, error)
}

type service struct {
	store    backupRepo.Store
	mongo    backupRepo.MongoRepository
	dataRoot string
	loc      *time.Location
	jobs     *jobTracker
}

// DefaultBackupDir 备份目录跟着部署挂载走：<exeDir>/files/backups
// （single-host-deployment.yml 把 ./data/files 挂到 /opt/ScopeSentry/files）。
func DefaultBackupDir() string {
	return filepath.Join(config.GlobalConfig.System.ExeDir, "files", backupsDirName)
}

// NewService 生产用构造器
func NewService() Service {
	return newService(DefaultBackupDir(), config.GlobalConfig.System.ExeDir)
}

// NewServiceWithPaths 指定备份目录与数据根目录（测试用，避免写入真实部署目录）。
func NewServiceWithPaths(backupDir, dataRoot string) Service {
	return newService(backupDir, dataRoot)
}

func newService(backupDir, dataRoot string) *service {
	loc, err := time.LoadLocation(config.GlobalConfig.System.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return &service{
		store:    backupRepo.NewStore(backupDir),
		mongo:    backupRepo.NewMongoRepository(),
		dataRoot: dataRoot,
		loc:      loc,
		jobs:     newJobTracker(),
	}
}

func (s *service) now() time.Time { return time.Now().In(s.loc) }

func (s *service) report(label string, progress int) {
	s.jobs.report(label, progress)
}

// Create 打一个全量备份包。
func (s *service) Create(ctx context.Context, note string) (info *models.BackupInfo, err error) {
	if !s.jobs.tryStart("create") {
		return nil, ErrJobRunning
	}
	defer func() {
		if r := recover(); r != nil {
			info, err = nil, fmt.Errorf("backup create panic: %v", r)
		}
		s.jobs.finish(err)
	}()
	return s.createBackup(ctx, strings.TrimSpace(note), "create")
}

// List 备份目录列表
func (s *service) List() ([]models.BackupInfo, error) {
	return s.store.List()
}

// Status 当前任务状态
func (s *service) Status() models.BackupStatus {
	return s.jobs.snapshot()
}

// Path 校验并返回备份包路径（下载/恢复共用）
func (s *service) Path(name string) (string, error) {
	return s.store.Path(name)
}

// Delete 删除备份包与旁车元数据
func (s *service) Delete(name string) error {
	return s.store.Delete(name)
}

// Restore 从备份包恢复：先自动做 pre-restore 备份，再逐集合 upsert。
func (s *service) Restore(ctx context.Context, name string) (result *models.BackupRestoreResult, err error) {
	archivePath, err := s.store.Path(name)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(archivePath); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
		}
		return nil, statErr
	}
	if !s.jobs.tryStart("restore") {
		return nil, ErrJobRunning
	}
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("backup restore panic: %v", r)
		}
		s.jobs.finish(err)
	}()
	return s.restoreLocked(ctx, name, archivePath)
}

// Upload 保存外部备份包并校验完整性（manifest 摘要 + 每个集合流的 sha256）。
func (s *service) Upload(ctx context.Context, filename string, r io.Reader, expectedSHA256 string) (info *models.BackupInfo, err error) {
	if !s.jobs.tryStart("upload") {
		return nil, ErrJobRunning
	}
	defer func() {
		if r := recover(); r != nil {
			info, err = nil, fmt.Errorf("backup upload panic: %v", r)
		}
		s.jobs.finish(err)
	}()
	return s.uploadLocked(ctx, filename, r, expectedSHA256)
}

// ================= 创建 =================

func (s *service) createBackup(ctx context.Context, note, label string) (*models.BackupInfo, error) {
	if err := s.store.Ensure(); err != nil {
		return nil, err
	}
	s.report(label, 2)

	staging, err := os.MkdirTemp(s.store.Dir(), ".staging-")
	if err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	collections, err := s.mongo.ListCollections(ctx)
	if err != nil {
		return nil, err
	}

	entries := make([]models.BackupCollectionEntry, 0, len(collections))
	for i, name := range collections {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := ValidateCollectionName(name); err != nil {
			return nil, fmt.Errorf("collection %q cannot be backed up: %w", name, err)
		}
		stagedPath := filepath.Join(staging, name+".bson")
		f, err := os.Create(stagedPath)
		if err != nil {
			return nil, fmt.Errorf("create staged collection %s: %w", name, err)
		}
		documents, sum, dumpErr := s.mongo.ExportCollection(ctx, name, f)
		closeErr := f.Close()
		if dumpErr != nil {
			return nil, dumpErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close staged collection %s: %w", name, closeErr)
		}
		entries = append(entries, models.BackupCollectionEntry{
			Name:      name,
			File:      CollectionFileName(name),
			Documents: documents,
			SHA256:    sum,
		})
		s.report(label, 2+int(float64(i+1)/float64(maxInt(len(collections), 1))*73))
	}

	dataFiles, uploads, files, err := s.collectDataFiles()
	if err != nil {
		return nil, err
	}
	s.report(label, 80)

	createdAt := s.now()
	manifest := FinalizeManifest(models.BackupManifest{
		Version:     BackupVersion,
		CreatedAt:   createdAt.Format(time.RFC3339),
		Collections: entries,
		Uploads:     uploads,
		Files:       files,
	})

	tmpArchive := filepath.Join(staging, "backup.tar.gz")
	if err := writeArchive(tmpArchive, staging, entries, dataFiles, manifest, createdAt); err != nil {
		return nil, err
	}
	s.report(label, 92)

	sum, err := s.store.HashFile(tmpArchive)
	if err != nil {
		return nil, fmt.Errorf("hash backup archive: %w", err)
	}
	stat, err := os.Stat(tmpArchive)
	if err != nil {
		return nil, fmt.Errorf("stat backup archive: %w", err)
	}

	name, err := s.store.UniqueName(createdAt.Format("20060102-150405"))
	if err != nil {
		return nil, err
	}
	finalPath, err := s.store.Path(name)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmpArchive, finalPath); err != nil {
		return nil, fmt.Errorf("move backup archive into place: %w", err)
	}

	info := &models.BackupInfo{
		Name:      name,
		Size:      stat.Size(),
		CreatedAt: manifest.CreatedAt,
		SHA256:    sum,
		Note:      note,
	}
	if err := s.store.SaveMeta(*info); err != nil {
		// 备份包本身有效，旁车文件失败只影响 note 展示，不阻塞返回。
		logger.Error(fmt.Sprintf("backup meta save failed for %s: %v", name, err))
	}
	s.report(label, 100)
	return info, nil
}

type dataFile struct {
	tarPath string
	srcPath string
	size    int64
}

// collectDataFiles 收集 uploads/ 与 files/（跳过 files/backups，避免备份包嵌套进自己）。
func (s *service) collectDataFiles() ([]dataFile, models.BackupDirSummary, models.BackupDirSummary, error) {
	uploadsDir := filepath.Join(s.dataRoot, "uploads")
	uploadsFiles, uploads, err := collectDir(uploadsDir, "uploads", nil)
	if err != nil {
		return nil, uploads, models.BackupDirSummary{}, fmt.Errorf("scan uploads dir: %w", err)
	}

	filesDir := filepath.Join(s.dataRoot, "files")
	skipBackups := func(p string) bool {
		return filepath.Clean(p) == filepath.Join(filesDir, backupsDirName)
	}
	filesFiles, files, err := collectDir(filesDir, "files", skipBackups)
	if err != nil {
		return nil, uploads, files, fmt.Errorf("scan files dir: %w", err)
	}

	all := make([]dataFile, 0, len(uploadsFiles)+len(filesFiles))
	all = append(all, uploadsFiles...)
	all = append(all, filesFiles...)
	return all, uploads, files, nil
}

func collectDir(root, tarRoot string, skip func(string) bool) ([]dataFile, models.BackupDirSummary, error) {
	summary := models.BackupDirSummary{}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, summary, nil
		}
		return nil, summary, err
	}
	if !info.IsDir() {
		return nil, summary, nil
	}

	var files []dataFile
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if skip != nil && skip(p) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		fileInfo, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, dataFile{
			tarPath: path.Join(tarRoot, filepath.ToSlash(relative)),
			srcPath: p,
			size:    fileInfo.Size(),
		})
		summary.Files++
		summary.Bytes += fileInfo.Size()
		return nil
	})
	if err != nil {
		return nil, summary, err
	}
	return files, summary, nil
}

// ================= 归档写入 / 读取 =================

func writeArchive(dst, staging string, entries []models.BackupCollectionEntry, dataFiles []dataFile, manifest models.BackupManifest, modTime time.Time) error {
	// 备份包里是整库数据（含口令哈希），落盘权限收紧到 0600。
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	fail := func(stepErr error) error {
		tw.Close()
		gz.Close()
		f.Close()
		return stepErr
	}

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fail(fmt.Errorf("marshal manifest: %w", err))
	}
	if err := writeTarEntry(tw, ManifestFileName, manifestData, modTime); err != nil {
		return fail(err)
	}
	for _, entry := range entries {
		if err := writeTarFile(tw, entry.File, filepath.Join(staging, entry.Name+".bson"), modTime); err != nil {
			return fail(err)
		}
	}
	for _, df := range dataFiles {
		if err := writeTarFile(tw, df.tarPath, df.srcPath, modTime); err != nil {
			return fail(err)
		}
	}

	if err := tw.Close(); err != nil {
		gz.Close()
		f.Close()
		return fmt.Errorf("close tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		f.Close()
		return fmt.Errorf("close gzip writer: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync archive: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close archive: %w", err)
	}
	return nil
}

func writeTarEntry(tw *tar.Writer, name string, data []byte, modTime time.Time) error {
	hdr := &tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(data)),
		ModTime:  modTime,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write tar entry %s: %w", name, err)
	}
	return nil
}

func writeTarFile(tw *tar.Writer, name, src string, modTime time.Time) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	hdr := &tar.Header{
		Name:     name,
		Mode:     int64(info.Mode().Perm()),
		Size:     info.Size(),
		ModTime:  modTime,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer f.Close()
	written, err := io.CopyN(tw, f, info.Size())
	if err != nil {
		return fmt.Errorf("copy %s into archive: %w", name, err)
	}
	if written != info.Size() {
		return fmt.Errorf("copy %s: expected %d bytes, wrote %d", name, info.Size(), written)
	}
	return nil
}

// readManifest 读取包内第一个条目（manifest.json）。
func readManifest(tr *tar.Reader) (*models.BackupManifest, error) {
	hdr, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read archive: %v", ErrManifestInvalid, err)
	}
	if path.Clean(hdr.Name) != ManifestFileName {
		return nil, fmt.Errorf("%w: first archive entry must be %s, got %s", ErrManifestInvalid, ManifestFileName, hdr.Name)
	}
	if hdr.Size > maxManifestSize {
		return nil, fmt.Errorf("%w: manifest is too large", ErrManifestInvalid)
	}
	data, err := io.ReadAll(io.LimitReader(tr, maxManifestSize))
	if err != nil {
		return nil, fmt.Errorf("%w: read manifest: %v", ErrManifestInvalid, err)
	}
	var manifest models.BackupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%w: decode manifest: %v", ErrManifestInvalid, err)
	}
	return &manifest, nil
}

// hashBSONStream 只扫长度前缀，计算流 sha256 与文档数（用于上传包校验，不落库）。
func hashBSONStream(r io.Reader) (int64, string, error) {
	hasher := sha256.New()
	var documents int64
	for {
		var lengthBuf [4]byte
		if _, err := io.ReadFull(r, lengthBuf[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return documents, "", fmt.Errorf("read bson document length: %w", err)
		}
		length := int64(binary.LittleEndian.Uint32(lengthBuf[:]))
		if length < 5 || length > maxDocumentSize {
			return documents, "", fmt.Errorf("invalid bson document length %d", length)
		}
		hasher.Write(lengthBuf[:])
		if _, err := io.CopyN(hasher, r, length-4); err != nil {
			return documents, "", fmt.Errorf("read bson document body: %w", err)
		}
		documents++
	}
	return documents, hex.EncodeToString(hasher.Sum(nil)), nil
}

// ================= 恢复 =================

func (s *service) restoreLocked(ctx context.Context, name, archivePath string) (*models.BackupRestoreResult, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %v", ErrManifestInvalid, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	manifest, err := readManifest(tr)
	if err != nil {
		return nil, err
	}
	if err := VerifyManifest(*manifest); err != nil {
		return nil, err
	}

	// 先确认包本身有效，再动库：坏包不该先触发一次全量 pre-restore 备份。
	s.report("pre-restore", 3)
	preRestore, err := s.createBackup(ctx, "pre-restore:"+name, "pre-restore")
	if err != nil {
		return nil, fmt.Errorf("pre-restore backup failed: %w", err)
	}
	s.report("restore", 15)

	pending := make(map[string]models.BackupCollectionEntry, len(manifest.Collections))
	for _, entry := range manifest.Collections {
		pending[entry.File] = entry
	}

	result := &models.BackupRestoreResult{
		Restored:   name,
		PreRestore: preRestore.Name,
	}
	uploadsSeen, filesSeen := 0, 0
	processed := 0
	total := maxInt(len(manifest.Collections), 1)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		clean, err := safeRelativePath(hdr.Name)
		if err != nil {
			return nil, err
		}
		if clean == ManifestFileName {
			continue
		}

		if entry, ok := pending[clean]; ok {
			if !isRegularTarEntry(hdr) {
				return nil, fmt.Errorf("%w: collection stream %s has unexpected type", ErrManifestInvalid, clean)
			}
			hasher := sha256.New()
			documents, err := s.mongo.RestoreCollection(ctx, entry.Name, io.TeeReader(tr, hasher))
			if err != nil {
				return nil, fmt.Errorf("restore collection %s: %w", entry.Name, err)
			}
			sum := hex.EncodeToString(hasher.Sum(nil))
			if documents != entry.Documents || sum != entry.SHA256 {
				return nil, fmt.Errorf("%w: collection %s content mismatch", ErrManifestInvalid, entry.Name)
			}
			delete(pending, clean)
			result.Collections++
			result.Documents += documents
			processed++
			s.report("restore", 15+int(float64(processed)/float64(total)*80))
			continue
		}

		switch {
		case strings.HasPrefix(clean, "uploads/"):
			if !isRegularTarEntry(hdr) {
				continue
			}
			if err := s.extractEntry(tr, hdr, clean); err != nil {
				return nil, err
			}
			uploadsSeen++
			result.Files++
		case strings.HasPrefix(clean, "files/"):
			if !isRegularTarEntry(hdr) {
				continue
			}
			if isBackupsEntry(clean) {
				return nil, fmt.Errorf("%w: archive must not contain backup archives (%s)", ErrManifestInvalid, clean)
			}
			if err := s.extractEntry(tr, hdr, clean); err != nil {
				return nil, err
			}
			filesSeen++
			result.Files++
		default:
			// 未知辅助条目（未来扩展）忽略，不影响已声明的数据。
			logger.Warn(fmt.Sprintf("backup restore: ignoring unknown archive entry %s", clean))
		}
	}

	// 把 gzip 尾部读完，强制校验 CRC/长度，防止只改 trailer 的坏包被放行。
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return nil, fmt.Errorf("%w: archive checksum: %v", ErrManifestInvalid, err)
	}

	if len(pending) > 0 {
		missing := make([]string, 0, len(pending))
		for file := range pending {
			missing = append(missing, file)
		}
		return nil, fmt.Errorf("%w: archive is missing %d collection streams: %s", ErrManifestInvalid, len(missing), strings.Join(missing, ", "))
	}
	if uploadsSeen != manifest.Uploads.Files || filesSeen != manifest.Files.Files {
		return nil, fmt.Errorf("%w: directory file count mismatch (uploads %d/%d, files %d/%d)",
			ErrManifestInvalid, uploadsSeen, manifest.Uploads.Files, filesSeen, manifest.Files.Files)
	}
	s.report("restore", 100)
	return result, nil
}

func isRegularTarEntry(hdr *tar.Header) bool {
	return hdr.Typeflag == tar.TypeReg
}

// safeRelativePath 归一化归档条目名，拒绝绝对路径与路径穿越。
func safeRelativePath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%w: invalid archive entry name", ErrManifestInvalid)
	}
	slash := filepath.ToSlash(name)
	if strings.HasPrefix(slash, "/") {
		return "", fmt.Errorf("%w: absolute archive entry %q", ErrManifestInvalid, name)
	}
	clean := path.Clean(slash)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: archive entry %q escapes root", ErrManifestInvalid, name)
	}
	return clean, nil
}

// isBackupsEntry 备份目录自身的文件永远不写回（避免旧包覆盖当前备份）。
func isBackupsEntry(clean string) bool {
	prefix := path.Join("files", backupsDirName)
	return clean == prefix || strings.HasPrefix(clean, prefix+"/")
}

// extractEntry 把 uploads/ 或 files/ 条目写回数据根目录。
func (s *service) extractEntry(r io.Reader, hdr *tar.Header, clean string) error {
	dst := filepath.Join(s.dataRoot, filepath.FromSlash(clean))
	rel, err := filepath.Rel(s.dataRoot, dst)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%w: archive entry %q escapes data root", ErrManifestInvalid, hdr.Name)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", clean, err)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", clean, err)
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", clean, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", clean, err)
	}
	return nil
}

// ================= 上传 =================

func (s *service) uploadLocked(ctx context.Context, filename string, r io.Reader, expectedSHA256 string) (*models.BackupInfo, error) {
	if err := s.store.Ensure(); err != nil {
		return nil, err
	}
	s.report("upload", 5)

	name := path.Base(filepath.ToSlash(strings.TrimSpace(filename)))
	if backupRepo.ValidateBackupName(name) != nil {
		generated, err := s.store.UniqueName(s.now().Format("20060102-150405"))
		if err != nil {
			return nil, err
		}
		name = generated
	}
	exists, err := s.store.Exists(name)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyExists, name)
	}

	tmp, err := os.CreateTemp(s.store.Dir(), ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("create upload temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	written, err := io.Copy(tmp, io.TeeReader(io.LimitReader(r, maxUploadSize+1), hasher))
	if err != nil {
		tmp.Close()
		return nil, fmt.Errorf("receive backup upload: %w", err)
	}
	if written > maxUploadSize {
		tmp.Close()
		return nil, fmt.Errorf("backup upload exceeds %d bytes", maxUploadSize)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("sync upload: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close upload: %w", err)
	}
	s.report("upload", 40)

	sum := hex.EncodeToString(hasher.Sum(nil))
	if expected := strings.TrimSpace(expectedSHA256); expected != "" && !strings.EqualFold(expected, sum) {
		return nil, fmt.Errorf("%w: expected %s got %s", ErrChecksumMismatch, expected, sum)
	}

	manifest, err := s.verifyArchive(ctx, tmpName)
	if err != nil {
		return nil, err
	}
	s.report("upload", 90)

	finalPath, err := s.store.Path(name)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		return nil, fmt.Errorf("move uploaded backup into place: %w", err)
	}

	info := &models.BackupInfo{
		Name:      name,
		Size:      written,
		CreatedAt: manifest.CreatedAt,
		SHA256:    sum,
		Note:      "uploaded",
	}
	if err := s.store.SaveMeta(*info); err != nil {
		logger.Error(fmt.Sprintf("backup meta save failed for uploaded %s: %v", name, err))
	}
	s.report("upload", 100)
	return info, nil
}

// verifyArchive 完整校验上传包：manifest 摘要 + 每个集合流 + 目录文件数。
func (s *service) verifyArchive(ctx context.Context, archivePath string) (*models.BackupManifest, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open uploaded archive: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %v", ErrManifestInvalid, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	manifest, err := readManifest(tr)
	if err != nil {
		return nil, err
	}
	if err := VerifyManifest(*manifest); err != nil {
		return nil, err
	}

	pending := make(map[string]models.BackupCollectionEntry, len(manifest.Collections))
	for _, entry := range manifest.Collections {
		pending[entry.File] = entry
	}
	uploadsSeen, filesSeen := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: read archive: %v", ErrManifestInvalid, err)
		}
		clean, err := safeRelativePath(hdr.Name)
		if err != nil {
			return nil, err
		}
		if clean == ManifestFileName {
			continue
		}
		if entry, ok := pending[clean]; ok {
			if !isRegularTarEntry(hdr) {
				return nil, fmt.Errorf("%w: collection stream %s has unexpected type", ErrManifestInvalid, clean)
			}
			documents, sum, err := hashBSONStream(tr)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
			}
			if documents != entry.Documents || sum != entry.SHA256 {
				return nil, fmt.Errorf("%w: collection %s content mismatch", ErrManifestInvalid, entry.Name)
			}
			delete(pending, clean)
			continue
		}
		switch {
		case strings.HasPrefix(clean, "uploads/"):
			if isRegularTarEntry(hdr) {
				uploadsSeen++
			}
		case strings.HasPrefix(clean, "files/"):
			if isBackupsEntry(clean) {
				return nil, fmt.Errorf("%w: archive must not contain backup archives (%s)", ErrManifestInvalid, clean)
			}
			if isRegularTarEntry(hdr) {
				filesSeen++
			}
		}
	}
	// 同上：读空 gzip 尾部，让 CRC 不一致的上传包在这里失败。
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return nil, fmt.Errorf("%w: archive checksum: %v", ErrManifestInvalid, err)
	}

	if len(pending) > 0 {
		return nil, fmt.Errorf("%w: archive is missing %d collection streams", ErrManifestInvalid, len(pending))
	}
	if uploadsSeen != manifest.Uploads.Files || filesSeen != manifest.Files.Files {
		return nil, fmt.Errorf("%w: directory file count mismatch", ErrManifestInvalid)
	}
	return manifest, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
