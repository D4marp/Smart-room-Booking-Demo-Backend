package server

import (
	"database/sql"

	"github.com/gin-gonic/gin"

	"github.com/bookify-rooms/backend/internal/config"
	"github.com/bookify-rooms/backend/internal/middleware"
)

type Server struct {
	cfg    *config.Config
	db     *sql.DB
	router *gin.Engine
}

func New(cfg *config.Config, db *sql.DB) *Server {
	s := &Server{cfg: cfg, db: db}
	s.setupRouter()
	return s
}

func (s *Server) Run() error {
	return s.router.Run(":" + s.cfg.Port)
}

func (s *Server) setupRouter() {
	r := gin.Default()

	r.Use(middleware.CORS(s.cfg.AllowedOrigins))
	r.Static("/uploads", s.cfg.UploadsDir)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "bookify-rooms-backend"})
	})

	s.registerRoutes(r)
	s.router = r
}
