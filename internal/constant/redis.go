package constant

const IncrTTLScript = `local c=redis.call('INCR',KEYS[1]) ` +
	`if c==1 then redis.call('EXPIRE',KEYS[1],ARGV[1]) end ` +
	`return {c,redis.call('TTL',KEYS[1])}`
