package api

import (
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "stylometry/stylometry_docs"
	"stylometry/stylometry_internal/stylometry_app/stylometry_auth"
	"stylometry/stylometry_internal/stylometry_app/stylometry_database"
	"stylometry/stylometry_internal/stylometry_app/stylometry_handler"
	"stylometry/stylometry_internal/stylometry_app/stylometry_repository"
)

func StartServer() {
	log.Println("StylometrySample application start!")
	issuer, err := auth.NewIssuerFromEnv()
	if err != nil {
		logrus.Fatal("ошибка конфигурации JWT: ", err)
	}

	db, err := database.NewDatabase()
	if err != nil {
		logrus.Fatal("ошибка подключения к базе данных стилометрии: ", err)
	}
	repo := repository.NewRepository(db)
	blacklist, err := auth.NewRedisBlacklistFromEnv()
	if err != nil {
		logrus.Fatal("ошибка подключения к Redis: ", err)
	}
	sessions, err := auth.NewRedisSessionStoreFromEnv()
	if err != nil {
		logrus.Fatal("ошибка подключения к Redis: ", err)
	}
	sampleHandler := handler.NewSampleHandler(repo, issuer, blacklist, sessions)
	requireJWT := auth.RequireSessionOrJWT(issuer, repo.UserExists, blacklist, sessions)
	optionalJWT := auth.OptionalSessionOrJWT(issuer, repo.UserExists, blacklist, sessions)

	router := gin.Default()
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	if stylometryFrontendEnabled() {
		router.LoadHTMLGlob("stylometry_templates/*")
		router.Static("/stylometry_static", "./stylometry_static")

		router.GET("/samples", sampleHandler.RedirectToSampleFeed)
		router.GET("/samples/feed", sampleHandler.GetSampleFeed)
		router.GET("/samples/draft", requireJWT, sampleHandler.GetSampleDraft)
		router.GET("/samples/grid", sampleHandler.GetSampleGrid)
		router.POST("/samples/draft", requireJWT, sampleHandler.PostSampleCreateDraft)
		router.POST("/samples/draft/publish", requireJWT, sampleHandler.PostSamplePublishDraft)
		router.POST("/samples/delete", requireJWT, sampleHandler.PostSampleDelete)
	}

	apiRoutes := router.Group("/api")
	{
		apiRoutes.GET("/samples", optionalJWT, sampleHandler.APIGetSampleList)
		apiRoutes.GET("/samples/feed", optionalJWT, sampleHandler.APIGetSampleFeed)
		apiRoutes.GET("/samples/draft", requireJWT, sampleHandler.APIGetSampleDraft)
		apiRoutes.POST("/samples", requireJWT, sampleHandler.APIPostSampleDraft)
		apiRoutes.PUT("/samples/draft/publish", requireJWT, sampleHandler.APIPutSamplePublishDraft)
		apiRoutes.DELETE("/samples/:id", requireJWT, sampleHandler.APIDeleteSample)
		apiRoutes.POST("/samples/:id/like", requireJWT, sampleHandler.APIPostSampleLike)
		apiRoutes.POST("/users", sampleHandler.APIPostUserRegister)
		apiRoutes.POST("/auth/login", sampleHandler.APIPostAuthLogin)
		apiRoutes.POST("/auth/logout", requireJWT, sampleHandler.APIPostAuthLogout)
	}
	log.Println("Server start up: http://127.0.0.1:8080/api/samples")
	if err := router.Run(":8080"); err != nil {
		logrus.Fatal(err)
	}
	log.Println("Server down")
}

func stylometryFrontendEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("ENABLE_FRONTEND")), "true")
}
