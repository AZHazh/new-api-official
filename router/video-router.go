package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	videoDashboardRouter := router.Group("/api/video-generation")
	videoDashboardRouter.Use(middleware.RouteTag("api"))
	videoDashboardRouter.Use(middleware.GlobalAPIRateLimit(), middleware.UserAuth())
	{
		videoDashboardRouter.GET("/models", controller.GetVideoGenerationModels)
		videoDashboardRouter.POST("/optimize", controller.OptimizeVideoPrompt)
	}

	seedanceSessionUploadRouter := router.Group("/pg")
	seedanceSessionUploadRouter.Use(middleware.RouteTag("relay"))
	seedanceSessionUploadRouter.Use(middleware.SystemPerformanceCheck())
	seedanceSessionUploadRouter.Use(middleware.UserAuth(), middleware.SeedanceUploadRateLimit())
	{
		seedanceSessionUploadRouter.POST("/files/upload", controller.SeedanceSessionFileUpload)
	}

	seedanceSessionSubmitRouter := router.Group("/pg")
	seedanceSessionSubmitRouter.Use(middleware.RouteTag("relay"))
	seedanceSessionSubmitRouter.Use(middleware.SystemPerformanceCheck())
	seedanceSessionSubmitRouter.Use(middleware.UserAuth(), middleware.ModelRequestRateLimit(), middleware.Distribute())
	{
		seedanceSessionSubmitRouter.POST("/video/generations", controller.SubmitVideoGeneration)
	}

	seedanceUploadRouter := router.Group("/v1")
	seedanceUploadRouter.Use(middleware.RouteTag("relay"))
	seedanceUploadRouter.Use(middleware.TokenAuth(), middleware.SeedanceUploadRateLimit())
	{
		seedanceUploadRouter.POST("/files/upload", controller.SeedanceFileUpload)
	}

	// Video proxy: accepts either session auth (dashboard) or token auth (API clients)
	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(middleware.VideoContentAuth())
	{
		videoProxyRouter.GET("/videos/:task_id/content", controller.VideoProxy)
	}

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		videoV1Router.POST("/video/generations", controller.RelayTask)
		videoV1Router.GET("/video/generations/:task_id", controller.RelayTaskFetch)
		videoV1Router.POST("/videos/:video_id/remix", controller.RelayTask)
	}
	// openai compatible API video routes
	// docs: https://platform.openai.com/docs/api-reference/videos/create
	{
		videoV1Router.POST("/videos", controller.RelayTask)
		videoV1Router.GET("/videos/:task_id", controller.RelayTaskFetch)
		videoV1Router.POST("/midjourney/generations/video", controller.RelayTask)
		videoV1Router.GET("/midjourney/tasks/:task_id", controller.RelayTaskFetch)
	}

	klingV1Router := router.Group("/kling/v1")
	klingV1Router.Use(middleware.RouteTag("relay"))
	klingV1Router.Use(middleware.KlingRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		klingV1Router.POST("/videos/text2video", controller.RelayTask)
		klingV1Router.POST("/videos/image2video", controller.RelayTask)
		klingV1Router.GET("/videos/text2video/:task_id", controller.RelayTaskFetch)
		klingV1Router.GET("/videos/image2video/:task_id", controller.RelayTaskFetch)
	}

	// Jimeng official API routes - direct mapping to official API format
	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(middleware.JimengRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		// Maps to: /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31 and /?Action=CVSync2AsyncGetResult&Version=2022-08-31
		jimengOfficialGroup.POST("/", controller.RelayTask)
	}
}
