# 🔧 Docker Build Troubleshooting Guide - SRS-PLN-NPS Backend

## ❌ Common Docker Build Failures & Solutions

### 1. **"Cannot connect to Docker daemon"**

**Error**:
```
Cannot connect to the Docker daemon at unix://...Is the docker daemon running?
```

**Solution**:
- Start Docker Desktop, OR
- If using Colima: `colima start`
- Verify: `docker --version`

---

### 2. **"Framework unknown, tidak ada volume otomatis"**

**Cause**: CI/CD system couldn't detect project framework

**Solutions**:
- Ensure all necessary files are in the repository:
  - ✅ `go.mod` 
  - ✅ `go.sum`
  - ✅ `Dockerfile`
  - ✅ `docker-compose.yml`
  - ✅ `cmd/server/main.go`

- Verify `.gitignore` doesn't exclude important files:
  ```bash
  # Should NOT be ignored:
  go.mod
  go.sum
  Dockerfile
  
  # Should be ignored:
  .env
  server (binary)
  uploads/
  ```

---

### 3. **"Docker Build Failed - Go compilation errors"**

**Common Causes**:

#### A. Go version mismatch
```
error: cannot use Go 1.25.5 with builder from 1.21
```

**Solution**:
```dockerfile
# Update Dockerfile
FROM golang:1.25-alpine AS builder  # Match go.mod version
```

Also update `go.mod`:
```go
go 1.25  // Not 1.25.5 (minor/patch only)
```

#### B. Missing go.mod updates
```
go: updates to go.mod needed
```

**Solution**:
```bash
cd backend
go mod tidy
go mod download
```

#### C. Missing dependencies
**Solution**:
```bash
go mod tidy
go get ./...
```

---

### 4. **"Dockerfile build fails: cannot find main.go"**

**Cause**: Build context is wrong or cmd/server/main.go missing

**Solution**:
```bash
# Verify file exists
ls -la backend/cmd/server/main.go

# Build from correct directory
cd backend
docker build -t pln_backend:latest .
```

---

### 5. **"Binary segfaults or won't start in container"**

**Cause**: Missing dependencies in final stage

**Solution**: Ensure final Dockerfile stage has:
```dockerfile
FROM alpine:latest
RUN apk --no-cache add ca-certificates wget
```

---

### 6. **"Health check keeps failing"**

**Error**:
```
health: starting health checks too early; not all endpoints ready
```

**Solution**:
```dockerfile
# Increase start-period
HEALTHCHECK --start-period=30s  # Give server time to start
```

Also verify endpoint exists:
```bash
# In container
wget --spider http://localhost:8080/health
```

---

## ✅ Complete Build Checklist

Before running `docker build`:

- [ ] Go version in go.mod matches Dockerfile
- [ ] `go.mod` and `go.sum` exist
- [ ] `go mod tidy` has been run
- [ ] `cmd/server/main.go` exists
- [ ] `.gitignore` doesn't exclude build files
- [ ] `.dockerignore` is minimal
- [ ] Dockerfile is in `backend/` directory
- [ ] No `.env` file (only `.env.example`)

---

## 🚀 Build Commands

### Build Backend Image Locally
```bash
cd backend

# Option 1: Using docker CLI directly
docker build -t pln_backend:latest .

# Option 2: Using docker-compose
docker-compose -f docker-compose.yml --env-file .env.docker build --no-cache

# Option 3: Verbose build with full output
docker build -t pln_backend:latest . --progress=plain
```

### Run Container After Build
```bash
docker run -d \
  --name pln_backend \
  -p 8080:8080 \
  -e DB_HOST=db \
  -e DB_PORT=3306 \
  -e DB_USER=bookify_user \
  -e DB_PASSWORD=bookify_secure_password \
  -e DB_NAME=bookify \
  pln_backend:latest
```

### With docker-compose (Preferred)
```bash
docker-compose -f docker-compose.yml --env-file .env.docker up --build
```

---

## 🔍 Debug Inside Container

### Access Container Shell
```bash
docker exec -it pln_backend sh
```

### Check Binary Exists
```bash
docker exec pln_backend ls -la /app/
```

### View Logs
```bash
docker logs pln_backend
docker logs -f pln_backend  # Follow logs
```

### Test Health Endpoint
```bash
docker exec pln_backend wget --spider http://localhost:8080/health
```

---

## 📊 Docker Image Size Optimization

### Current Multi-stage Setup
```
builder stage (1GB+)  ─┐
                      ├─→ final (25-50MB)
Alpine runtime (5MB)  ─┘
```

To check image size:
```bash
docker images | grep pln
# pln_backend    latest    <size>
```

To reduce further:
1. Use `distroless` base image (no shell, minimal deps)
2. Strip Go binary: `go build -ldflags="-s -w"`
3. Use `upx` to compress binary

---

## ✅ Pre-Push Verification

Before committing Docker changes:

```bash
# 1. Run local build
cd backend && docker build -t test:latest .

# 2. Run container
docker run --rm test:latest ./server --version

# 3. Test with docker-compose
docker-compose -f docker-compose.yml --env-file .env.docker build

# 4. Cleanup
docker system prune -a
```

---

## 🚢 CI/CD Deployment

When deploying via CI/CD (GitHub Actions, etc):

```yaml
# .github/workflows/docker-build.yml
name: Docker Build

on: [push, pull_request]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: docker/setup-buildx-action@v2
      - uses: docker/build-push-action@v4
        with:
          context: ./backend
          push: false
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

**Key points for CI/CD**:
- Ensure repository has all required files (go.mod, Dockerfile, etc)
- Don't include `.env` file (only `.env.example`)
- Use multi-stage builds to minimize image size
- Include health checks in Dockerfile
- Test build artifacts before push

---

## 📞 Support

If Docker build still fails:

1. Check logs carefully: `docker build ... --progress=plain`
2. Verify all files are committed: `git status`
3. Test locally before pushing
4. Check GitHub Actions logs for detailed error

---

## 📝 Recent Fixes (May 24, 2026)

✅ Updated Go version in Dockerfile to 1.25 (to match go.mod)
✅ Added `/health` endpoint for robust health checks
✅ Improved `.dockerignore` configuration
✅ Added Alpine package `wget` for health check binary
✅ Created `uploads/` directory in container
✅ Increased health check `start-period` to 30s
