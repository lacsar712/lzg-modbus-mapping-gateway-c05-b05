package httpapi

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/bytecode/modbus-mapping-gateway/internal/usecase"
)

type Server struct {
	svc    *usecase.GatewayService
	secret []byte
}

func NewServer(svc *usecase.GatewayService) *Server {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "modbus-mapping-gateway-demo-secret-32b"
	}
	return &Server{svc: svc, secret: []byte(secret)}
}

func (s *Server) Router() *gin.Engine {
	r := gin.Default()
	r.GET("/api/health", s.health)

	api := r.Group("/api")
	api.POST("/auth/login", s.login)

	auth := api.Group("")
	auth.Use(s.authMiddleware())
	{
		auth.GET("/devices", s.listDevices)
		auth.GET("/devices/:id/points", s.listPoints)
		auth.GET("/devices/:id/points/:name", s.getPoint)
		auth.PUT("/devices/:id/points/:name", s.writePoint)
		auth.GET("/devices/:id/snapshot", s.snapshot)
		auth.GET("/mapping", s.getMapping)
		auth.POST("/reload", s.reload)
	}
	return r
}

type claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

var users = map[string]struct {
	Password string
	Role     string
}{
	"engineer": {Password: "mod123456", Role: "engineer"},
	"observer": {Password: "obs123456", Role: "observer"},
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	u, ok := users[req.Username]
	if !ok || u.Password != req.Password {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Username: req.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	signed, err := token.SignedString(s.secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token":    signed,
		"username": req.Username,
		"role":     u.Role,
	})
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}
		raw := strings.TrimPrefix(h, "Bearer ")
		parsed, err := jwt.ParseWithClaims(raw, &claims{}, func(t *jwt.Token) (interface{}, error) {
			return s.secret, nil
		})
		if err != nil || !parsed.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		cl, ok := parsed.Claims.(*claims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Set("username", cl.Username)
		c.Set("role", cl.Role)
		c.Next()
	}
}

func (s *Server) requireEngineer(c *gin.Context) bool {
	role, _ := c.Get("role")
	if role != "engineer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "engineer role required"})
		return false
	}
	return true
}

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, s.svc.Health())
}

func (s *Server) listDevices(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"devices": s.svc.ListDevices()})
}

func (s *Server) listPoints(c *gin.Context) {
	pts, err := s.svc.GetPoints(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"points": pts})
}

func (s *Server) getPoint(c *gin.Context) {
	_, p, err := s.svc.GetPoint(c.Param("id"), c.Param("name"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (s *Server) writePoint(c *gin.Context) {
	if !s.requireEngineer(c) {
		return
	}
	var req struct {
		Value float64 `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body, expect {value}"})
		return
	}
	if err := s.svc.WritePoint(c.Param("id"), c.Param("name"), req.Value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) snapshot(c *gin.Context) {
	snap, err := s.svc.Snapshot(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, snap)
}

func (s *Server) getMapping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"yaml": s.svc.YAMLText()})
}

func (s *Server) reload(c *gin.Context) {
	if !s.requireEngineer(c) {
		return
	}
	var req struct {
		YAML string `json:"yaml"`
	}
	_ = c.ShouldBindJSON(&req)
	var err error
	if strings.TrimSpace(req.YAML) != "" {
		err = s.svc.ReloadFromText(req.YAML)
	} else {
		err = s.svc.Reload()
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   err.Error(),
			"keptOld": true,
			"yaml":    s.svc.YAMLText(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"devices": len(s.svc.ListDevices()),
		"yaml":    s.svc.YAMLText(),
	})
}
