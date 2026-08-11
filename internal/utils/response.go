package utils

import "github.com/gin-gonic/gin"

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func Success(c *gin.Context, statusCode int, data interface{}) {
	c.JSON(statusCode, APIResponse{Success: true, Data: data})
}

func SuccessMessage(c *gin.Context, statusCode int, message string, data interface{}) {
	c.JSON(statusCode, APIResponse{Success: true, Message: message, Data: data})
}

func Error(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, APIResponse{Success: false, Error: message})
}
