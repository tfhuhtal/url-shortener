package shortener

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const cacheTTL = 24 * time.Hour

type Service struct {
	repo  *Repository
	redis *redis.Client
}

func NewService(repo *Repository, redisClient *redis.Client) *Service {
	return &Service{repo: repo, redis: redisClient}
}

func (s *Service) Shorten(ctx context.Context, longURL string) (URLRecord, error) {
	normalizedURL, err := normalizeURL(longURL)
	if err != nil {
		return URLRecord{}, err
	}

	if cachedShortCode, cacheErr := s.redis.Get(ctx, longToShortKey(normalizedURL)).Result(); cacheErr == nil {
		if record, dbErr := s.repo.GetByShortCode(ctx, cachedShortCode); dbErr == nil {
			return record, nil
		}
	}

	const maxRetries = 5
	for i := range maxRetries {
		candidate := generateShortCode(normalizedURL, i)
		record, dbErr := s.repo.CreateOrGet(ctx, candidate, normalizedURL)
		if dbErr != nil {
			if IsShortCodeConflict(dbErr) {
				continue
			}
			return URLRecord{}, dbErr
		}

		s.cacheRecord(ctx, record)
		return record, nil
	}

	return URLRecord{}, fmt.Errorf("failed to generate unique short code")
}

func (s *Service) Resolve(ctx context.Context, shortCode string) (string, error) {
	if shortCode == "" {
		return "", ErrNotFound
	}

	if cachedURL, cacheErr := s.redis.Get(ctx, shortToLongKey(shortCode)).Result(); cacheErr == nil {
		return cachedURL, nil
	}

	record, err := s.repo.GetByShortCode(ctx, shortCode)
	if err != nil {
		return "", err
	}

	s.cacheRecord(ctx, record)
	return record.LongURL, nil
}

func (s *Service) cacheRecord(ctx context.Context, record URLRecord) {
	_ = s.redis.Set(ctx, shortToLongKey(record.ShortCode), record.LongURL, cacheTTL).Err()
	_ = s.redis.Set(ctx, longToShortKey(record.LongURL), record.ShortCode, cacheTTL).Err()
}

func normalizeURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("url must include scheme and host")
	}

	return parsed.String(), nil
}

func generateShortCode(longURL string, salt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", longURL, salt)))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	return encoded[:8]
}

func shortToLongKey(shortCode string) string {
	return "short:" + shortCode
}

func longToShortKey(longURL string) string {
	return "long:" + longURL
}
