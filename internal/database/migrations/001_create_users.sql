CREATE TABLE IF NOT EXISTS users (
    id            VARCHAR(36) PRIMARY KEY,
    name          VARCHAR(255) NOT NULL,
    email         VARCHAR(255) UNIQUE NOT NULL,
    password      VARCHAR(255) NOT NULL,
    profile_image VARCHAR(500),
    city          VARCHAR(100),
    role          VARCHAR(20) NOT NULL DEFAULT 'user',
    created_at    BIGINT NOT NULL,
    updated_at    BIGINT
);

CREATE INDEX idx_users_email ON users(email);

CREATE INDEX idx_users_role ON users(role)
