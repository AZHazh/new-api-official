package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func SetMultimediaRouter(router *gin.Engine) {
	group := router.Group("/api/multimedia")
	group.Use(middleware.RouteTag("api"), middleware.GlobalAPIRateLimit(), middleware.UserAuth(), controller.RequireMultimediaSession)
	group.GET("/models", controller.GetMultimediaModels)
	group.GET("/generations", controller.GetMultimediaGenerations)
	group.POST("/generations", controller.SubmitMultimediaImage)
	group.GET("/assets", controller.GetMultimediaAssets)
	group.POST("/assets", controller.UploadMultimediaImage)
	group.GET("/assets/:id/content", controller.GetMultimediaAssetContent)
	group.POST("/assets/remove", controller.RemoveMultimediaAssets)
	group.POST("/assets/download", controller.DownloadMultimediaAssets)
}
