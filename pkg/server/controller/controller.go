package controller

import "github.com/gin-gonic/gin"

// Controller interface used as a marker or base.
type Controller interface{}

// SystemController handles system-wide operations.
type SystemController struct{}

func NewSystemController() *SystemController {
	return &SystemController{}
}

func (ctrl *SystemController) HealthCheck(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}
