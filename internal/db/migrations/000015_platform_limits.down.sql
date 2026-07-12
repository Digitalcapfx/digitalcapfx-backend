-- Migration 000015 (down): drop admin-controllable limits.
DROP TABLE IF EXISTS user_limit_overrides;
DROP TABLE IF EXISTS platform_limits;
