-- Atomically refills and deducts tokens for one Redis key.
-- Returns {allowed, remaining, limit, retry_after_ms}.
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])

local now = redis.call('TIME')
local now_ms = now[1] * 1000 + math.floor(now[2] / 1000)

local data = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(data[1])
local last_refill = tonumber(data[2])

if tokens == nil then
  tokens = capacity
  last_refill = now_ms
end

local elapsed = (now_ms - last_refill) / 1000.0
if elapsed > 0 then
  tokens = math.min(capacity, tokens + elapsed * rate)
  last_refill = now_ms
end

local allowed = 0
local remaining = math.floor(tokens)
local retry_after = 0

if requested <= 0 then
  allowed = 1
else
  if tokens >= requested then
    tokens = tokens - requested
    allowed = 1
    remaining = math.floor(tokens)
  else
    remaining = 0
    if rate > 0 then
      local missing = requested - tokens
      retry_after = math.ceil(missing / rate * 1000)
    end
  end
end

redis.call('HMSET', key, 'tokens', tokens, 'last_refill', last_refill)

local ttl_sec = math.ceil(capacity / rate) + 60
redis.call('EXPIRE', key, ttl_sec)

return {allowed, remaining, capacity, retry_after}
