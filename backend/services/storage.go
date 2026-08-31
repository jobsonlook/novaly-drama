package services

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Storage struct {
	Root string
	COS  *COSStorage
}

func NewStorage(root string, cos *COSStorage) *Storage {
	return &Storage{Root: root, COS: cos}
}

func (s *Storage) COSEnabled() bool {
	return s != nil && s.COS != nil && s.COS.Enabled()
}

// ObjectKey converts a local path under Root (or already-relative key) to a COS object key.
func (s *Storage) ObjectKey(localPath string) string {
	localPath = filepath.ToSlash(localPath)
	root := filepath.ToSlash(s.Root)
	if strings.HasPrefix(localPath, root+"/") {
		return strings.TrimPrefix(localPath, root+"/")
	}
	if strings.HasPrefix(localPath, "data/uploads/") {
		return strings.TrimPrefix(localPath, "data/uploads/")
	}
	if strings.HasPrefix(localPath, "/api/uploads/") {
		return strings.TrimPrefix(localPath, "/api/uploads/")
	}
	return strings.TrimPrefix(localPath, "/")
}

func (s *Storage) putCOS(localPath string, data []byte) error {
	if !s.COSEnabled() {
		return nil
	}
	return s.COS.Put(s.ObjectKey(localPath), data, contentTypeForKey(localPath))
}

func isVideoPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".webm", ".mov", ".m4v":
		return true
	default:
		return false
	}
}

func (s *Storage) FileExists(localPath string) bool {
	if localPath == "" {
		return false
	}
	// Videos are COS-canonical when COS is enabled: ignore leftover local files
	// so PublicURL never points at a missing object.
	if s.COSEnabled() && isVideoPath(localPath) {
		key := s.ObjectKey(localPath)
		return key != "" && s.COS.Exists(key)
	}
	if _, err := os.Stat(localPath); err == nil {
		return true
	}
	if s.COSEnabled() {
		key := s.ObjectKey(localPath)
		return key != "" && s.COS.Exists(key)
	}
	return false
}

func (s *Storage) DeleteFile(localPath string) {
	if localPath == "" {
		return
	}
	_ = os.Remove(localPath)
	if s.COSEnabled() {
		_ = s.COS.Delete(s.ObjectKey(localPath))
	}
}

func (s *Storage) ResourceImagePath(projectID, resourceID uint, ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "jpg"
	}
	return filepath.Join(s.Root, fmt.Sprintf("projects/%d/resources/%d.%s", projectID, resourceID, ext))
}

func (s *Storage) ResourceVideoPath(projectID, resourceID uint, ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "mp4"
	}
	return filepath.Join(s.Root, fmt.Sprintf("projects/%d/resources/%d.%s", projectID, resourceID, ext))
}

func (s *Storage) ShotVideoPath(projectID, shotID uint, ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "mp4"
	}
	return filepath.Join(s.Root, fmt.Sprintf("projects/%d/videos/%d.%s", projectID, shotID, ext))
}

// BindCOSObject verifies an object already exists on COS and drops any stale local
// cache at the same path. Direct uploads write COS only; without this, ReadFile /
// download would keep serving the old local bytes while the player uses the COS URL.
func (s *Storage) BindCOSObject(localPath string) error {
	if !s.COSEnabled() {
		return fmt.Errorf("COS 未配置")
	}
	key := s.ObjectKey(localPath)
	if !s.COS.Exists(key) {
		return fmt.Errorf("云存储中未找到文件：%s", key)
	}
	_ = os.Remove(localPath)
	return nil
}

func (s *Storage) SaveResourceImage(projectID, resourceID uint, data string) (string, error) {
	dir := filepath.Join(s.Root, fmt.Sprintf("projects/%d/resources", projectID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	raw, ext, err := decodeImage(data)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.%s", resourceID, ext))
	if err = os.WriteFile(path, raw, 0644); err != nil {
		return "", err
	}
	if err = s.putCOS(path, raw); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Storage) SaveResourceImageBytes(projectID, resourceID uint, data []byte) (string, error) {
	dir := filepath.Join(s.Root, fmt.Sprintf("projects/%d/resources", projectID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.jpg", resourceID))
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	if err := s.putCOS(path, data); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Storage) SaveStylizedImageBytes(projectID, resourceID uint, data []byte) (string, error) {
	dir := filepath.Join(s.Root, fmt.Sprintf("projects/%d/resources", projectID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d_stylized.jpg", resourceID))
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	if err := s.putCOS(path, data); err != nil {
		// Local file is already durable; keep retrying COS in background so stylize
		// isn't blocked by a transient upload timeout to the bucket.
		log.Printf("COS stylized upload deferred for %s: %v", path, err)
		go func(p string, payload []byte) {
			for i := 1; i <= 3; i++ {
				time.Sleep(time.Duration(i) * 3 * time.Second)
				if err := s.putCOS(p, payload); err == nil {
					log.Printf("COS stylized upload recovered for %s (attempt %d)", p, i)
					return
				} else if i == 3 {
					log.Printf("COS stylized upload still failing for %s: %v", p, err)
				}
			}
		}(path, append([]byte(nil), data...))
	}
	return path, nil
}

func (s *Storage) StylizedPublicURL(projectID, resourceID uint) string {
	key := fmt.Sprintf("projects/%d/resources/%d_stylized.jpg", projectID, resourceID)
	local := filepath.Join(s.Root, key)
	// Prefer local when present so a deferred COS sync does not 404 the UI.
	if _, err := os.Stat(local); err == nil {
		return "/api/uploads/" + key
	}
	if s.COSEnabled() {
		return s.COS.PublicURL(key)
	}
	return "/api/uploads/" + key
}

func (s *Storage) SaveResourceVideoBytes(projectID, resourceID uint, data []byte, ext string) (string, error) {
	return s.saveResourceVideo(projectID, resourceID, data, ext, "")
}

// SaveResourceVideoCopyFrom stores a resource video on COS (copy when possible).
// When COS is enabled, no server-local video file is kept.
func (s *Storage) SaveResourceVideoCopyFrom(projectID, resourceID uint, data []byte, ext, copyFromLocal string) (string, error) {
	return s.saveResourceVideo(projectID, resourceID, data, ext, copyFromLocal)
}

func (s *Storage) saveResourceVideo(projectID, resourceID uint, data []byte, ext, copyFromLocal string) (string, error) {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "mp4"
	}
	path := s.ResourceVideoPath(projectID, resourceID, ext)

	if s.COSEnabled() {
		if copyFromLocal != "" {
			if err := s.COS.Copy(s.ObjectKey(copyFromLocal), s.ObjectKey(path)); err != nil {
				if len(data) == 0 {
					return "", fmt.Errorf("COS 复制视频失败：%w", err)
				}
				if err := s.putCOS(path, data); err != nil {
					return "", fmt.Errorf("COS 上传视频失败：%w", err)
				}
			}
		} else {
			if len(data) == 0 {
				return "", fmt.Errorf("视频数据为空")
			}
			if err := s.putCOS(path, data); err != nil {
				return "", fmt.Errorf("COS 上传视频失败：%w", err)
			}
		}
		_ = os.Remove(path)
		return path, nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Storage) SaveVideo(projectID, shotID uint, data []byte, ext string) (string, error) {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "mp4"
	}
	path := s.ShotVideoPath(projectID, shotID, ext)

	// Videos are COS-only when configured — do not keep a server-local copy.
	if s.COSEnabled() {
		if len(data) == 0 {
			return "", fmt.Errorf("视频数据为空")
		}
		if err := s.putCOS(path, data); err != nil {
			return "", fmt.Errorf("COS 上传视频失败：%w", err)
		}
		_ = os.Remove(path)
		return path, nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

// SaveVideoFromCOSKey copies an existing COS object into the shot video key.
// No local file is kept — bytes stay on COS until something actually reads them.
func (s *Storage) SaveVideoFromCOSKey(projectID, shotID uint, srcKey, ext string) (string, error) {
	if !s.COSEnabled() {
		return "", fmt.Errorf("COS 未配置")
	}
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	if ext == "" {
		ext = "mp4"
	}
	path := s.ShotVideoPath(projectID, shotID, ext)
	dstKey := s.ObjectKey(path)
	if err := s.COS.Copy(srcKey, dstKey); err != nil {
		return "", err
	}
	_ = os.Remove(path)
	return path, nil
}

// FindShotVideo locates a shot video on COS (preferred) or disk without downloading the body.
func (s *Storage) FindShotVideo(projectID, shotID uint) (path, ext string, ok bool) {
	dir := filepath.Join(s.Root, fmt.Sprintf("projects/%d/videos", projectID))
	for _, e := range []string{"mp4", "webm", "mov", "m4v"} {
		p := filepath.Join(dir, fmt.Sprintf("%d.%s", shotID, e))
		if s.COSEnabled() && s.COS.Exists(s.ObjectKey(p)) {
			return p, e, true
		}
		if !s.COSEnabled() {
			if _, err := os.Stat(p); err == nil {
				return p, e, true
			}
		}
	}
	return "", "", false
}

// FileSize returns COS Content-Length for videos when COS is enabled; otherwise local size.
func (s *Storage) FileSize(path string) (int64, error) {
	if path == "" {
		return 0, os.ErrNotExist
	}
	if s.COSEnabled() && isVideoPath(path) {
		return s.COS.ObjectSize(s.ObjectKey(path))
	}
	if info, err := os.Stat(path); err == nil {
		return info.Size(), nil
	}
	if !s.COSEnabled() {
		return 0, os.ErrNotExist
	}
	return s.COS.ObjectSize(s.ObjectKey(path))
}

func (s *Storage) ReadShotVideo(projectID, shotID uint) ([]byte, string, error) {
	dir := filepath.Join(s.Root, fmt.Sprintf("projects/%d/videos", projectID))
	for _, ext := range []string{"mp4", "webm", "mov", "m4v"} {
		path := filepath.Join(dir, fmt.Sprintf("%d.%s", shotID, ext))
		data, err := s.ReadFile(path)
		if err == nil {
			return data, ext, nil
		}
	}
	return nil, "", os.ErrNotExist
}

func (s *Storage) PublicURL(kind string, projectID, id uint, ext string) string {
	if ext == "" {
		ext = "jpg"
	}
	key := fmt.Sprintf("projects/%d/%s/%d.%s", projectID, kind, id, ext)
	if s.COSEnabled() {
		return s.COS.PublicURL(key)
	}
	return "/api/uploads/" + key
}

func (s *Storage) ResourcePublicURL(projectID, resourceID uint, imagePath string) string {
	ext := "jpg"
	if i := strings.LastIndex(imagePath, "."); i >= 0 {
		ext = imagePath[i+1:]
	}
	return s.PublicURL("resources", projectID, resourceID, ext)
}

func AbsolutePublicURL(baseURL, path string) string {
	if baseURL == "" || path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimSuffix(baseURL, "/") + path
}

func (s *Storage) SaveTempReferenceImage(projectID uint, data string) (string, error) {
	dir := filepath.Join(s.Root, fmt.Sprintf("projects/%d/refs", projectID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	raw, ext, err := decodeImage(data)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d.%s", time.Now().UnixNano(), ext)
	path := filepath.Join(dir, name)
	if err = os.WriteFile(path, raw, 0644); err != nil {
		return "", err
	}
	if err = s.putCOS(path, raw); err != nil {
		return "", err
	}
	key := fmt.Sprintf("projects/%d/refs/%s", projectID, name)
	if s.COSEnabled() {
		return s.COS.PublicURL(key), nil
	}
	return "/api/uploads/" + key, nil
}

func (s *Storage) ReadFile(path string) ([]byte, error) {
	if !s.COSEnabled() {
		return os.ReadFile(path)
	}
	key := s.ObjectKey(path)

	// Videos: COS is the source of truth; drop any leftover local cache.
	if isVideoPath(path) {
		cosData, cosErr := s.COS.Get(key)
		if cosErr == nil {
			_ = os.Remove(path)
			return cosData, nil
		}
		local, localErr := os.ReadFile(path)
		if localErr == nil {
			return local, nil
		}
		return nil, cosErr
	}

	local, localErr := os.ReadFile(path)
	if localErr == nil {
		cosSize, cosErr := s.COS.ObjectSize(key)
		// Local cache matches COS (or COS unreachable) — serve local.
		if cosErr != nil || cosSize == int64(len(local)) {
			return local, nil
		}
		// Diverged: direct upload may have overwritten COS while leaving stale local bytes.
		cosData, cosErr := s.COS.Get(key)
		if cosErr != nil {
			return local, nil
		}
		_ = os.Remove(path)
		return cosData, nil
	}
	cosData, cosErr := s.COS.Get(key)
	if cosErr != nil {
		return nil, localErr
	}
	return cosData, nil
}

func (s *Storage) ImageDataURL(path string) (string, error) {
	raw, err := s.ReadFile(path)
	if err != nil {
		return "", err
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	mime := "image/jpeg"
	switch ext {
	case "png":
		mime = "image/png"
	case "webp":
		mime = "image/webp"
	case "gif":
		mime = "image/gif"
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(raw)), nil
}

func DecodeImageData(data string) ([]byte, string, error) {
	return decodeImage(data)
}

func (s *Storage) ImageBytes(path string) ([]byte, string, error) {
	raw, err := s.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		ext = "jpg"
	}
	return raw, ext, nil
}

// RewriteUploadURL turns legacy /api/uploads/... paths into COS public URLs when enabled.
func (s *Storage) RewriteUploadURL(u string) string {
	if u == "" || !s.COSEnabled() {
		return u
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		// Already absolute; rewrite only our own upload proxy URLs.
		if i := strings.Index(u, "/api/uploads/"); i >= 0 {
			return s.COS.PublicURL(strings.TrimPrefix(u[i:], "/api/uploads/"))
		}
		return u
	}
	const prefix = "/api/uploads/"
	if strings.HasPrefix(u, prefix) {
		return s.COS.PublicURL(strings.TrimPrefix(u, prefix))
	}
	return u
}

func decodeImage(data string) ([]byte, string, error) {
	ext := "jpg"
	if strings.HasPrefix(data, "data:") {
		parts := strings.SplitN(data, ",", 2)
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("无效的图片数据")
		}
		if strings.Contains(parts[0], "png") {
			ext = "png"
		} else if strings.Contains(parts[0], "webp") {
			ext = "webp"
		}
		raw, err := base64.StdEncoding.DecodeString(parts[1])
		return raw, ext, err
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	return raw, ext, err
}
