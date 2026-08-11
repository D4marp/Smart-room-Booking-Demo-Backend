package config

import (
	"log"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type Config struct {
	Port               string
	DatabaseURL        string
	JWTSecret          string
	JWTExpiry          string // e.g. "168h"
	UploadsDir         string
	BaseURL            string
	AllowedOrigins     []string
	SuperAdminEmail    string
	SuperAdminPassword string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	databaseURL := resolveDatabaseURL()
	if databaseURL == "" {
		databaseURL = "root:@tcp(127.0.0.1:3306)/bookify?parseTime=true&charset=utf8mb4"
	}

	logDatabaseTarget(databaseURL)

	return &Config{
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        databaseURL,
		JWTSecret:          getEnv("JWT_SECRET", "change-this-secret-in-production"),
		JWTExpiry:          getEnv("JWT_EXPIRY", "168h"),
		UploadsDir:         getEnv("UPLOADS_DIR", "./uploads"),
		BaseURL:            getEnv("BASE_URL", "http://localhost:8080"),
		AllowedOrigins:     []string{getEnv("ALLOWED_ORIGINS", "*")},
		SuperAdminEmail:    getEnv("SUPERADMIN_EMAIL", ""),
		SuperAdminPassword: getEnv("SUPERADMIN_PASSWORD", ""),
	}
}

func resolveDatabaseURL() string {
	for _, key := range []string{"DATABASE_URL", "MYSQL_URL", "MYSQL_DSN"} {
		if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
			if dsn := normalizeDatabaseURL(raw); dsn != "" {
				return dsn
			}
			log.Printf("Ignoring invalid %s value; falling back to DB_* variables", key)
		}
	}

	if dsn := buildDatabaseURL(); dsn != "" {
		return dsn
	}

	return ""
}

func normalizeDatabaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if isGoMySQLDSN(raw) {
		return ensureDSNParams(raw)
	}

	if strings.HasPrefix(raw, "mysql://") || strings.HasPrefix(raw, "mysql2://") {
		return mysqlURLToDSN(raw)
	}

	// PaaS often injects only "host:port" into DATABASE_URL.
	if !strings.Contains(raw, "@") && strings.Contains(raw, ":") {
		host, port, err := net.SplitHostPort(raw)
		if err == nil {
			return buildDatabaseURLWithParts(host, port)
		}
	}

	// Some platforms provide "user:pass@host:port/db" without tcp().
	if strings.Contains(raw, "@") && strings.Contains(raw, "/") && !strings.Contains(raw, "@tcp(") {
		at := strings.LastIndex(raw, "@")
		userInfo := raw[:at]
		rest := raw[at+1:]

		slash := strings.Index(rest, "/")
		if slash == -1 {
			return ""
		}

		addr := rest[:slash]
		dbName := strings.TrimPrefix(rest[slash+1:], "/")
		if idx := strings.Index(dbName, "?"); idx >= 0 {
			dbName = dbName[:idx]
		}

		user, password := splitUserInfo(userInfo)
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
			port = "3306"
		}

		return formatMySQLDSN(user, password, host, port, dbName)
	}

	return ""
}

func buildDatabaseURL() string {
	host := firstEnv("DB_HOST", "MYSQL_HOST", "MYSQLHOST")
	port := firstEnv("DB_PORT", "MYSQL_PORT", "MYSQLPORT")
	name := firstEnv("DB_NAME", "MYSQL_DATABASE", "MYSQLDATABASE")
	user := firstEnv("DB_USER", "MYSQL_USER", "MYSQLUSER")
	password := firstEnv("DB_PASSWORD", "MYSQL_PASSWORD", "MYSQLPASSWORD")

	if host == "" {
		host, port = hostPortFromURLVars(port)
	}

	if host == "" || name == "" {
		return ""
	}

	host, port = normalizeHostPort(host, port)
	return formatMySQLDSN(user, password, host, port, name)
}

func hostPortFromURLVars(defaultPort string) (string, string) {
	for _, key := range []string{"DATABASE_URL", "MYSQL_URL", "MYSQL_DSN"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" || strings.Contains(raw, "@") {
			continue
		}

		host, port, err := net.SplitHostPort(raw)
		if err != nil {
			continue
		}

		if defaultPort == "" {
			defaultPort = port
		}

		return host, defaultPort
	}

	return "", defaultPort
}

func buildDatabaseURLWithParts(host, port string) string {
	name := firstEnv("DB_NAME", "MYSQL_DATABASE", "MYSQLDATABASE")
	user := firstEnv("DB_USER", "MYSQL_USER", "MYSQLUSER")
	password := firstEnv("DB_PASSWORD", "DB_PASS", "MYSQL_PASSWORD", "MYSQLPASSWORD")

	if name == "" || user == "" {
		return ""
	}

	host, port = normalizeHostPort(host, port)
	return formatMySQLDSN(user, password, host, port, name)
}

func mysqlURLToDSN(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	user := ""
	password := ""
	if parsed.User != nil {
		user = parsed.User.Username()
		password, _ = parsed.User.Password()
	}

	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "3306"
	}

	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		dbName = firstEnv("DB_NAME", "MYSQL_DATABASE", "MYSQLDATABASE")
	}

	if host == "" || dbName == "" {
		return ""
	}

	return formatMySQLDSN(user, password, host, port, dbName)
}

func formatMySQLDSN(user, password, host, port, dbName string) string {
	cfg := mysql.Config{
		User:                 user,
		Passwd:               password,
		Net:                  "tcp",
		Addr:                 net.JoinHostPort(host, port),
		DBName:               dbName,
		ParseTime:            true,
		AllowNativePasswords: true,
		CheckConnLiveness:    true,
		Params: map[string]string{
			"charset": "utf8mb4",
		},
	}

	return cfg.FormatDSN()
}

func normalizeHostPort(host, port string) (string, string) {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)

	if host == "" {
		if port == "" {
			return "", "3306"
		}
		return "", port
	}

	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		if port == "" {
			port = p
		}
	}

	if port == "" {
		port = "3306"
	}

	return host, port
}

func isGoMySQLDSN(raw string) bool {
	return strings.Contains(raw, "@tcp(") || strings.Contains(raw, "@unix(")
}

func ensureDSNParams(raw string) string {
	if strings.Contains(raw, "parseTime=") {
		return raw
	}

	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}

	return raw + sep + "parseTime=true&charset=utf8mb4"
}

func splitUserInfo(userInfo string) (string, string) {
	if userInfo == "" {
		return "", ""
	}

	if idx := strings.Index(userInfo, ":"); idx >= 0 {
		return userInfo[:idx], userInfo[idx+1:]
	}

	return userInfo, ""
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func logDatabaseTarget(dsn string) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		log.Printf("Database target configured (DSN parse failed for logging: %v)", err)
		return
	}

	user := cfg.User
	if user == "" {
		user = "(empty)"
	}

	log.Printf("Connecting to database at %s/%s as %s", cfg.Addr, cfg.DBName, user)
}
