package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go-monorepo.com/pkg/message"
)

func main() {
	r := gin.Default()
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": message.Svc1,
		})
	})
	if err := r.Run(); err != nil {
		panic(err)
	}
}
