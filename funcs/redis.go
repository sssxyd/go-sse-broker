package funcs

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	client redis.UniversalClient
}

// NewRedisClient 创建一个新的 Redis 客户端实例。
//
// 参数：
//   - addrs []string：Redis 服务器地址列表。
//   - password string：Redis 访问密码。
//   - db int：数据库索引。
//   - poolSize int：连接池大小。
//
// 返回值：
//   - *RedisClient：Redis 客户端实例。
//   - string：本地 IP 和端口。
//   - error：错误信息，如果操作成功则为 nil。
func NewRedisClient(addrs []string, password string, db int, poolSize int) (*RedisClient, string, int, error) {
	var client redis.UniversalClient
	var localIP string = "127.0.0.1"
	var localPort int = 0
	isCluster := len(addrs) > 1
	dialer := &net.Dialer{}

	if isCluster {
		// 集群模式
		clusterClient := redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    addrs,
			Password: password,
			PoolSize: poolSize, // 连接池大小
			Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, addr)
				if err == nil {
					localAddr := conn.LocalAddr().(*net.TCPAddr)
					localIP = localAddr.IP.String()
					localPort = localAddr.Port
				}
				return conn, err
			},
		})
		client = clusterClient
	} else {
		// 单例模式
		singleClient := redis.NewClient(&redis.Options{
			Addr:     addrs[0],
			Password: password,
			DB:       db,
			PoolSize: poolSize, // 连接池大小
			Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, addr)
				if err == nil {
					localAddr := conn.LocalAddr().(*net.TCPAddr)
					localIP = localAddr.IP.String()
					localPort = localAddr.Port
				}
				return conn, err
			},
		})
		client = singleClient
	}

	// 测试连接是否成功
	err := client.Ping(context.Background()).Err()
	if err != nil {
		return nil, localIP, localPort, err
	}

	return &RedisClient{client: client}, localIP, localPort, nil
}

// Set 设置指定键的值。
//
// 参数：
//   - key string：键。
//   - value interface{}：值。
//   - expiration time.Duration：过期时间。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Set(key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(context.Background(), key, value, expiration).Err()
}

// Get 获取指定键的值。
//
// 参数：
//   - key string：键。
//
// 返回值：
//   - string：值。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Get(key string) (string, error) {
	return r.client.Get(context.Background(), key).Result()
}

// MGet 获取多个键的值。
//
// 参数：
//   - keys ...string：键列表。
//
// 返回值：
//   - []interface{}：值列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) MGet(keys ...string) ([]interface{}, error) {
	return r.client.MGet(context.Background(), keys...).Result()
}

// Del 删除指定键。
//
// 参数：
//   - key string：键。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Del(key string) error {
	return r.client.Del(context.Background(), key).Err()
}

// Exists 检查指定键是否存在。
//
// 参数：
//   - key string：键。
//
// 返回值：
//   - bool：指定键是否存在。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Exists(key string) (bool, error) {
	exists, err := r.client.Exists(context.Background(), key).Result()
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

// Expire 设置指定键的过期时间。
//
// 参数：
//   - key string：键。
//   - expiration time.Duration：过期时间。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Expire(key string, expiration time.Duration) error {
	return r.client.Expire(context.Background(), key, expiration).Err()
}

// TTL 获取指定键的剩余过期时间。
//
// 参数：
//   - key string：键。
//
// 返回值：
//   - time.Duration：剩余过期时间。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) TTL(key string) (time.Duration, error) {
	return r.client.TTL(context.Background(), key).Result()
}

// Incr 将指定键的值增加 1。
//
// 参数：
//   - key string：键。
//
// 返回值：
//   - int64：增加后的值。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Incr(key string) (int64, error) {
	return r.client.Incr(context.Background(), key).Result()
}

// IncrBy 将指定键的值增加 value。
//
// 参数：
//   - key string：键。
//   - value int64：增加的值。
//
// 返回值：
//   - int64：增加后的值。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) IncrBy(key string, value int64) (int64, error) {
	return r.client.IncrBy(context.Background(), key, value).Result()
}

// Decr 将指定键的值减少 1。
//
// 参数：
//   - key string：键。
//
// 返回值：
//   - int64：减少后的值。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Decr(key string) (int64, error) {
	return r.client.Decr(context.Background(), key).Result()
}

// DecrBy 将指定键的值减少 value。
//
// 参数：
//   - key string：键。
//   - value int64：减少的值。
//
// 返回值：
//   - int64：减少后的值。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) DecrBy(key string, value int64) (int64, error) {
	return r.client.DecrBy(context.Background(), key, value).Result()
}

// HSet 设置指定哈希表中指定字段的值。
//
// 参数：
//   - key string：哈希表键。
//   - values ...interface{}：字段和值列表，依次为字段1、值1、字段2、值2……。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HSet(key string, values ...interface{}) error {
	return r.client.HSet(context.Background(), key, values).Err()
}

// HSetWithMap 设置指定哈希表中的字段和值。
//
// 参数：
//   - key string：哈希表键。
//   - values map[string]interface{}：字段和值的映射。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HSetWithMap(key string, values map[string]interface{}) error {
	return r.client.HMSet(context.Background(), key, values).Err()
}

// HGet 获取指定哈希表中指定字段的值。
//
// 参数：
//   - key string：哈希表键。
//   - field string：字段。
//
// 返回值：
//   - string：值。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HGet(key, field string) (string, error) {
	return r.client.HGet(context.Background(), key, field).Result()
}

// HDel 删除指定哈希表中的一个或多个字段。
//
// 参数：
//   - key string：哈希表键。
//   - fields ...string：字段列表。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HDel(key string, fields ...string) error {
	return r.client.HDel(context.Background(), key, fields...).Err()
}

// HExists 检查指定哈希表中指定字段是否存在。
//
// 参数：
//   - key string：哈希表键。
//   - field string：字段。
//
// 返回值：
//   - bool：指定字段是否存在。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HExists(key, field string) (bool, error) {
	return r.client.HExists(context.Background(), key, field).Result()
}

// HGetAll 获取指定哈希表的所有字段和值。
//
// 参数：
//   - key string：哈希表键。
//
// 返回值：
//   - map[string]string：字段和值的映射。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HGetAll(key string) (map[string]string, error) {
	return r.client.HGetAll(context.Background(), key).Result()
}

// HIncrBy 将指定哈希表中指定字段的值增加指定增量。
//
// 参数：
//   - key string：哈希表键。
//   - field string：字段。
//   - incr int64：增量。
//
// 返回值：
//   - int64：增加后的值。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HIncrBy(key, field string, incr int64) (int64, error) {
	return r.client.HIncrBy(context.Background(), key, field, incr).Result()
}

// HKeys 获取指定哈希表的所有字段。
//
// 参数：
//   - key string：哈希表键。
//
// 返回值：
//   - []string：字段列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HKeys(key string) ([]string, error) {
	return r.client.HKeys(context.Background(), key).Result()
}

// HLen 获取指定哈希表的字段数量。
//
// 参数：
//   - key string：哈希表键。
//
// 返回值：
//   - int64：字段数量。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HLen(key string) (int64, error) {
	return r.client.HLen(context.Background(), key).Result()
}

// HSetNX 仅当指定哈希表中指定字段不存在时，设置其值。
//
// 参数：
//   - key string：哈希表键。
//   - field string：字段。
//   - value interface{}：值。
//
// 返回值：
//   - bool：是否设置成功。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HSetNX(key, field string, value interface{}) (bool, error) {
	return r.client.HSetNX(context.Background(), key, field, value).Result()
}

// HVals 获取指定哈希表的所有值。
//
// 参数：
//   - key string：哈希表键。
//
// 返回值：
//   - []string：值列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) HVals(key string) ([]string, error) {
	return r.client.HVals(context.Background(), key).Result()
}

// LIndex 获取指定列表中指定索引的元素。
//
// 参数：
//   - key string：列表键。
//   - index int64：索引。
//
// 返回值：
//   - string：元素。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LIndex(key string, index int64) (string, error) {
	return r.client.LIndex(context.Background(), key, index).Result()
}

// LInsert 在指定列表中的某个元素前或后插入一个新元素。
//
// 参数：
//   - key string：列表键。
//   - op string：插入操作，可选值为 "BEFORE" 或 "AFTER"。
//   - pivot interface{}：参考元素。
//   - value interface{}：新元素。
//
// 返回值：
//   - int64：插入后列表的长度。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LInsert(key string, op string, pivot interface{}, value interface{}) (int64, error) {
	return r.client.LInsert(context.Background(), key, op, pivot, value).Result()
}

// LLen 获取指定列表的长度。
//
// 参数：
//   - key string：列表键。
//
// 返回值：
//   - int64：列表长度。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LLen(key string) (int64, error) {
	return r.client.LLen(context.Background(), key).Result()
}

// LPop 移除并返回指定列表的第一个元素。
//
// 参数：
//   - key string：列表键。
//
// 返回值：
//   - string：第一个元素。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LPop(key string) (string, error) {
	return r.client.LPop(context.Background(), key).Result()
}

// LPush 将一个或多个元素插入到指定列表的头部。
//
// 参数：
//   - key string：列表键。
//   - values ...interface{}：要插入的元素列表。
//
// 返回值：
//   - int64：插入后列表的长度。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LPush(key string, values ...interface{}) (int64, error) {
	return r.client.LPush(context.Background(), key, values...).Result()
}

// LRange 获取指定列表的指定范围内的元素。
//
// 参数：
//   - key string：列表键。
//   - start int64：起始索引。
//   - stop int64：结束索引。
//
// 返回值：
//   - []string：元素列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LRange(key string, start int64, stop int64) ([]string, error) {
	return r.client.LRange(context.Background(), key, start, stop).Result()
}

// LRem 从指定列表中移除指定数量的指定元素。
//
// 参数：
//   - key string：列表键。
//   - count int64：移除数量，正数表示从头部开始，负数表示从尾部开始。
//   - value interface{}：要移除的元素。
//
// 返回值：
//   - int64：实际移除的元素数量。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LRem(key string, count int64, value interface{}) (int64, error) {
	return r.client.LRem(context.Background(), key, count, value).Result()
}

// LSet 设置指定列表中指定索引的元素值。
//
// 参数：
//   - key string：列表键。
//   - index int64：索引。
//   - value interface{}：新值。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LSet(key string, index int64, value interface{}) error {
	return r.client.LSet(context.Background(), key, index, value).Err()
}

// LTrim 修剪指定列表，只保留指定范围内的元素。
//
// 参数：
//   - key string：列表键。
//   - start int64：起始索引。
//   - stop int64：结束索引。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) LTrim(key string, start int64, stop int64) error {
	return r.client.LTrim(context.Background(), key, start, stop).Err()
}

// RPop 移除并返回指定列表的最后一个元素。
//
// 参数：
//   - key string：列表键。
//
// 返回值：
//   - string：最后一个元素。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) RPop(key string) (string, error) {
	return r.client.RPop(context.Background(), key).Result()
}

// RPush 将一个或多个元素插入到指定列表的尾部。
//
// 参数：
//   - key string：列表键。
//   - values ...interface{}：要插入的元素列表。
//
// 返回值：
//   - int64：插入后列表的长度。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) RPush(key string, values ...interface{}) (int64, error) {
	return r.client.RPush(context.Background(), key, values...).Result()
}

// SAdd 向指定集合中添加一个或多个元素。
//
// 参数：
//   - key string：集合键。
//   - members ...interface{}：要添加的元素列表。
//
// 返回值：
//   - int64：实际添加的元素数量。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SAdd(key string, members ...interface{}) (int64, error) {
	return r.client.SAdd(context.Background(), key, members...).Result()
}

// SCard 获取指定集合的元素数量。
//
// 参数：
//   - key string：集合键。
//
// 返回值：
//   - int64：元素数量。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SCard(key string) (int64, error) {
	return r.client.SCard(context.Background(), key).Result()
}

// SDiff 返回指定集合之间的差集。
//
// 参数：
//   - keys ...string：集合键列表。
//
// 返回值：
//   - []string：差集元素列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SDiff(keys ...string) ([]string, error) {
	return r.client.SDiff(context.Background(), keys...).Result()
}

// SInter 返回指定集合之间的交集。
//
// 参数：
//   - keys ...string：集合键列表。
//
// 返回值：
//   - []string：交集元素列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SInter(keys ...string) ([]string, error) {
	return r.client.SInter(context.Background(), keys...).Result()
}

// SIsMember 检查指定元素是否存在于指定集合中。
//
// 参数：
//   - key string：集合键。
//   - member interface{}：元素。
//
// 返回值：
//   - bool：指定元素是否存在。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SIsMember(key string, member interface{}) (bool, error) {
	return r.client.SIsMember(context.Background(), key, member).Result()
}

// SMembers 获取指定集合的所有元素。
//
// 参数：
//   - key string：集合键。
//
// 返回值：
//   - []string：元素列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SMembers(key string) ([]string, error) {
	return r.client.SMembers(context.Background(), key).Result()
}

// SMove 将指定集合中的指定元素从源集合移动到目标集合。
//
// 参数：
//   - src string：源集合键。
//   - dst string：目标集合键。
//   - member interface{}：要移动的元素。
//
// 返回值：
//   - bool：是否移动成功。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SMove(src string, dst string, member interface{}) (bool, error) {
	return r.client.SMove(context.Background(), src, dst, member).Result()
}

// SPop 移除并返回指定集合的一个随机元素。
//
// 参数：
//   - key string：集合键。
//
// 返回值：
//   - string：随机元素。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SPop(key string) (string, error) {
	return r.client.SPop(context.Background(), key).Result()
}

// SRem 从指定集合中移除一个或多个元素。
//
// 参数：
//   - key string：集合键。
//   - members ...interface{}：要移除的元素列表。
//
// 返回值：
//   - int64：实际移除的元素数量。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SRem(key string, members ...interface{}) (int64, error) {
	return r.client.SRem(context.Background(), key, members...).Result()
}

// SUnion 返回指定集合之间的并集。
//
// 参数：
//   - keys ...string：集合键列表。
//
// 返回值：
//   - []string：并集元素列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) SUnion(keys ...string) ([]string, error) {
	return r.client.SUnion(context.Background(), keys...).Result()
}

// ZAdd 向有序集合中添加一个或多个成员。
//
// 参数：
//   - key string：有序集合键。
//   - members ...*redis.Z：要添加的成员列表。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZAdd(key string, members ...*redis.Z) error {
	zMembers := make([]redis.Z, len(members))
	for i, m := range members {
		zMembers[i] = *m
	}
	return r.client.ZAdd(context.Background(), key, zMembers...).Err()
}

// ZAddNX 仅当指定成员不存在时，向有序集合中添加一个或多个成员。
//
// 参数：
//   - key string：有序集合键。
//   - members ...*redis.Z：要添加的成员列表。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZAddNX(key string, members ...*redis.Z) error {
	zMembers := make([]redis.Z, len(members))
	for i, m := range members {
		zMembers[i] = *m
	}
	return r.client.ZAddNX(context.Background(), key, zMembers...).Err()
}

// ZCard 获取有序集合的成员数量。
//
// 参数：
//   - key string：有序集合键。
//
// 返回值：
//   - int64：成员数量。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZCard(key string) (int64, error) {
	return r.client.ZCard(context.Background(), key).Result()
}

// ZCount 获取有序集合中指定分数范围内的成员数量。
//
// 参数：
//   - key string：有序集合键。
//   - min string：最小分数。
//   - max string：最大分数。
//
// 返回值：
//   - int64：成员数量。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZCount(key string, min string, max string) (int64, error) {
	return r.client.ZCount(context.Background(), key, min, max).Result()
}

// ZIncrBy 将有序集合中指定成员的分数增加指定增量。
//
// 参数：
//   - key string：有序集合键。
//   - increment float64：增量。
//   - member string：成员。
//
// 返回值：
//   - float64：增加后的分数。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZIncrBy(key string, increment float64, member string) (float64, error) {
	return r.client.ZIncrBy(context.Background(), key, increment, member).Result()
}

// ZRange 获取有序集合中指定范围内的成员，按排名升序。
//
// 参数：
//   - key string：有序集合键。
//   - start int64：起始索引。
//   - stop int64：结束索引。
//
// 返回值：
//   - []string：成员列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZRange(key string, start int64, stop int64) ([]string, error) {
	return r.client.ZRange(context.Background(), key, start, stop).Result()
}

// ZRangeByScore 根据分数范围获取有序集合的成员。
//
// 参数：
//   - key string：有序集合键。
//   - min string：最小分数。
//   - max string：最大分数。
//
// 返回值：
//   - []string：成员列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZRangeByScore(key string, min string, max string) ([]string, error) {
	return r.client.ZRangeByScore(context.Background(), key, &redis.ZRangeBy{
		Min: min,
		Max: max,
	}).Result()
}

// ZRangeByScoreWithScores 根据分数范围获取有序集合的成员及其分数。
//
// 参数：
//   - key string：有序集合键。
//   - min string：最小分数。
//   - max string：最大分数。
//
// 返回值：
//   - []redis.Z：成员及其分数列表。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZRangeByScoreWithScores(key string, min string, max string) ([]redis.Z, error) {
	return r.client.ZRangeByScoreWithScores(context.Background(), key, &redis.ZRangeBy{
		Min: min,
		Max: max,
	}).Result()
}

// ZRem 从有序集合中移除指定成员。
//
// 参数：
//   - key string：有序集合键。
//   - members ...interface{}：要移除的成员列表。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZRem(key string, members ...interface{}) error {
	return r.client.ZRem(context.Background(), key, members...).Err()
}

// ZScore 获取成员的分数。
//
// 参数：
//   - key string：有序集合键。
//   - member string：成员。
//
// 返回值：
//   - float64：成员的分数。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZScore(key string, member string) (float64, error) {
	return r.client.ZScore(context.Background(), key, member).Result()
}

// ZRank 获取成员的排名（升序）
//
// 参数：
//   - key string：有序集合键。
//   - member string：成员。
//
// 返回值：
//   - int64：成员的排名。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZRank(key string, member string) (int64, error) {
	return r.client.ZRank(context.Background(), key, member).Result()
}

// ZRemRangeByScore 移除分数范围内的成员
//
// 参数：
//   - key string：有序集合键。
//   - min string：最小分数。
//   - max string：最大分数。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZRemRangeByScore(key string, min string, max string) error {
	return r.client.ZRemRangeByScore(context.Background(), key, min, max).Err()
}

// ZRemRangeByRank 移除排名范围内的成员
//
// 参数：
//   - key string：有序集合键。
//   - start int64：起始索引。
//   - stop int64：结束索引。
//
// 返回值：
//   - int64：实际移除的成员数量。
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) ZRemRangeByRank(key string, start int64, stop int64) (int64, error) {
	return r.client.ZRemRangeByRank(context.Background(), key, start, stop).Result()
}

// Publish 向指定频道发布消息
//
// 参数：
//   - channel string：频道。
//   - message interface{}：消息。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Publish(channel string, message interface{}) error {
	ctx := context.Background()
	return r.client.Publish(ctx, channel, message).Err()
}

// Subscribe 订阅一个或多个频道，并处理接收到的消息, 该方法会阻塞当前 goroutine。
//
// 参数：
//   - ctx context.Context：带cancel的context。
//   - handler func(channel string, payload string)：消息处理函数。
//   - channels ...string：频道列表。
//
// 返回值：
//   - error：错误信息，如果操作成功则为 nil。
func (r *RedisClient) Subscribe(ctx context.Context, handler func(channel string, payload string), channels ...string) error {
	log.Printf("Subscribe Redis Channels: %v", channels)
	pubsub := r.client.Subscribe(ctx, channels...)

	// 等待订阅确认
	_, err := pubsub.Receive(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			// 取消订阅并关闭 pubsub
			pubsub.Close()
			log.Printf("Subscribe canceled: %v", channels)
			return nil
		case msg, ok := <-pubsub.Channel():
			// 检查订阅通道是否关闭
			if !ok {
				log.Printf("Subscribe closed: %v", channels)
				return nil
			}
			log.Printf("Message: %v\n", msg)
			handler(msg.Channel, msg.Payload)
		}
	}
}

func (r *RedisClient) GetClient() redis.UniversalClient {
	return r.client
}

func (r *RedisClient) Pipeline() redis.Pipeliner {
	return r.client.Pipeline()
}

func (r *RedisClient) TxPipelined(ctx context.Context, f func(redis.Pipeliner) error) ([]redis.Cmder, error) {
	return r.client.TxPipelined(ctx, f)
}

func (r *RedisClient) Watch(ctx context.Context, f func(*redis.Tx) error, keys ...string) error {
	return r.client.Watch(ctx, f, keys...)
}

func (r *RedisClient) Client() redis.UniversalClient {
	return r.client
}

func (r *RedisClient) Close() error {
	return r.client.Close()
}
