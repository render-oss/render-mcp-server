package session

import (
	"context"
	"errors"
	"net/url"

	"github.com/redis/go-redis/v9"
	"github.com/render-oss/render-mcp-server/pkg/config"
)

type redisStore struct {
	c *redis.Client
}

var _ Store = (*redisStore)(nil)

func NewRedisStore(addr string) (Store, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}

	addr = u.Host

	o := &redis.Options{
		Addr: u.Host,
	}
	if u.User != nil {
		o.Username = u.User.Username()
		o.Password, _ = u.User.Password()
	}
	return &redisStore{
		c: redis.NewClient(o),
	}, nil
}

func (r *redisStore) Get(ctx context.Context, sessionID string) (Session, error) {
	return &RedisSession{
		c:         r.c,
		sessionID: sessionID,
	}, nil
}

type RedisSession struct {
	c         *redis.Client
	sessionID string
}

var _ Session = (*RedisSession)(nil)

const workspaceField = "workspaceID"

func (r *RedisSession) GetWorkspace(ctx context.Context) (string, error) {
	val, err := r.c.HGet(ctx, r.sessionKey(), workspaceField).Result()
	if errors.Is(err, redis.Nil) {
		return "", config.ErrNoWorkspace
	} else if err != nil {
		return "", err
	}
	return val, nil
}

func (r *RedisSession) SetWorkspace(ctx context.Context, s string) error {
	return r.c.HSet(ctx, r.sessionKey(), workspaceField, s).Err()
}

func (r *RedisSession) sessionKey() string {
	return "session:" + r.sessionID
}
