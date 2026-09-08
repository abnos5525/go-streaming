package httpserver

import (
	"log"

	"github.com/gin-gonic/gin"
	"project1/internal/auth"
	"project1/internal/config"
	"project1/internal/controllers"
	"project1/internal/swagger"
)

func Run() {
	config.Load()
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	swagger.Register(r)
	controllers.Register(r)
	if err := auth.Check(); err != nil {
		log.Printf("hafez connection failed — %v", err)
	} else {
		log.Print("hafez connected")
	}
	log.Print("server running at http://127.0.0.1:8085")
	log.Print("swagger at http://127.0.0.1:8085/api/doc")
	log.Print("POST /stream/start  then /play/{id}")
	log.Fatal(r.Run(":8085"))
}
