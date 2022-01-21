// Copyright 2017 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package cache

import (
	"fmt"
	"strconv"

	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"

	mc "gitea.com/go-chi/cache"

	_ "gitea.com/go-chi/cache/memcache" // memcache plugin for cache
)

var conn mc.Cache

func newCache(cacheConfig setting.Cache) (mc.Cache, error) {
	return mc.NewCacher(mc.Options{
		Adapter:       cacheConfig.Adapter,
		AdapterConfig: cacheConfig.Conn,
		Interval:      cacheConfig.Interval,
	})
}

// NewContext start cache service
func NewContext() error {
	var err error

	if conn == nil && setting.CacheService.Enabled {
		if conn, err = newCache(setting.CacheService.Cache); err != nil {
			return err
		}
		const testKey = "__gitea_cache_test"
		const testVal = "test-value"
		if err = conn.Put(testKey, testVal, 10); err != nil {
			return err
		}
		val := conn.Get(testKey)
		if valStr, ok := val.(string); !ok || valStr != testVal {
			// If the cache is full, the Get may not read the expected value stored by Put.
			// Since we have checked that Put can success, so we just show a warning here, do not return an error to panic.
			log.Warn("cache (adapter:%s, config:%s) doesn't seem to work correctly, set test value '%v' but get '%v'",
				setting.CacheService.Cache.Adapter, setting.CacheService.Cache.Conn,
				testVal, val,
			)
		}
	}

	return err
}

// GetCache returns the currently configured cache
func GetCache() mc.Cache {
	return conn
}

// GetString returns the key value from cache with callback when no key exists in cache
func GetString(key string, getFunc func() (string, error)) (string, error) {
	if conn == nil || setting.CacheService.TTL == 0 {
		return getFunc()
	}
	if !conn.IsExist(key) {
		var (
			value string
			err   error
		)
		if value, err = getFunc(); err != nil {
			return value, err
		}
		err = conn.Put(key, value, setting.CacheService.TTLSeconds())
		if err != nil {
			return "", err
		}
	}
	value := conn.Get(key)
	if v, ok := value.(string); ok {
		return v, nil
	}
	if v, ok := value.(fmt.Stringer); ok {
		return v.String(), nil
	}
	return fmt.Sprintf("%s", conn.Get(key)), nil
}

// GetInt returns key value from cache with callback when no key exists in cache
func GetInt(key string, getFunc func() (int, error)) (int, error) {
	if conn == nil || setting.CacheService.TTL == 0 {
		return getFunc()
	}
	if !conn.IsExist(key) {
		var (
			value int
			err   error
		)
		if value, err = getFunc(); err != nil {
			return value, err
		}
		err = conn.Put(key, value, setting.CacheService.TTLSeconds())
		if err != nil {
			return 0, err
		}
	}
	switch value := conn.Get(key).(type) {
	case int:
		return value, nil
	case string:
		v, err := strconv.Atoi(value)
		if err != nil {
			return 0, err
		}
		return v, nil
	default:
		return 0, fmt.Errorf("Unsupported cached value type: %v", value)
	}
}

// GetInt64 returns key value from cache with callback when no key exists in cache
func GetInt64(key string, getFunc func() (int64, error)) (int64, error) {
	if conn == nil || setting.CacheService.TTL == 0 {
		return getFunc()
	}
	if !conn.IsExist(key) {
		var (
			value int64
			err   error
		)
		if value, err = getFunc(); err != nil {
			return value, err
		}
		err = conn.Put(key, value, setting.CacheService.TTLSeconds())
		if err != nil {
			return 0, err
		}
	}
	switch value := conn.Get(key).(type) {
	case int64:
		return value, nil
	case string:
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, err
		}
		return v, nil
	default:
		return 0, fmt.Errorf("Unsupported cached value type: %v", value)
	}
}

// Remove key from cache
func Remove(key string) {
	if conn == nil {
		return
	}
	_ = conn.Delete(key)
}
