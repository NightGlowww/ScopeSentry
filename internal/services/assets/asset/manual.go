package asset

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrAssetExists 资产已存在（host+port 唯一索引冲突）
var ErrAssetExists = errors.New("asset already exists")

// ErrAssetInvalid 手工录入参数不合法
var ErrAssetInvalid = errors.New("invalid asset")

// AddAsset 手工录入一条资产（issue #203）。
//
// 资产平时由扫描节点直接写库；这里给「外部 recon 结果、人工确认过的主机」一个入口。
// host+port 是唯一索引，重复录入返回 ErrAssetExists 而不是 500。
func (s *service) AddAsset(ctx *gin.Context, req *models.AssetAddRequest) (*models.Asset, error) {
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return nil, fmt.Errorf("%w: host is required", ErrAssetInvalid)
	}
	port := strings.TrimSpace(req.Port)
	urlValue := strings.TrimSpace(req.URL)
	assetType := ""
	if urlValue != "" {
		parsed, err := url.Parse(urlValue)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("%w: url must be absolute", ErrAssetInvalid)
		}
		assetType = parsed.Scheme
	}

	assetDoc := &models.Asset{
		Host:    host,
		IP:      strings.TrimSpace(req.IP),
		Port:    port,
		URL:     urlValue,
		Service: strings.TrimSpace(req.Service),
		Title:   strings.TrimSpace(req.Title),
		Project: strings.TrimSpace(req.Project),
		Tags:    req.Tags,
		Type:    assetType,
		Time:    helper.GetNowTimeString(),
	}
	if err := s.assetRepo.CreateAsset(ctx.Request.Context(), assetDoc); err != nil {
		if isDuplicateKey(err) {
			return nil, ErrAssetExists
		}
		return nil, err
	}
	return assetDoc, nil
}

// isDuplicateKey 判断是不是 host+port 唯一索引冲突。
func isDuplicateKey(err error) bool {
	return mongo.IsDuplicateKeyError(err)
}
