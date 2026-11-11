package handler

import (
	"errors"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"stylometry/stylometry_internal/stylometry_app/stylometry_analysis"
	"stylometry/stylometry_internal/stylometry_app/stylometry_auth"
	"stylometry/stylometry_internal/stylometry_app/stylometry_media"
	"stylometry/stylometry_internal/stylometry_app/stylometry_password"
	"stylometry/stylometry_internal/stylometry_app/stylometry_serializer"
)

func stylometryAPIFormFile(stylometry_ctx *gin.Context, stylometry_names ...string) (*multipart.FileHeader, error) {
	for _, stylometry_name := range stylometry_names {
		stylometry_file, stylometry_err := stylometry_ctx.FormFile(stylometry_name)
		if stylometry_err == nil {
			return stylometry_file, nil
		}
		if !errors.Is(stylometry_err, http.ErrMissingFile) {
			return nil, stylometry_err
		}
	}
	return nil, http.ErrMissingFile
}

type stylometryAPILikeRequest struct {
	StylometryLike *int `form:"stylometry_like" json:"stylometry_like"`
	Like           *int `form:"like" json:"like"`
}

type stylometryAPIUserRequest struct {
	Name     string `form:"stylometry_sample_name" json:"stylometry_sample_name"`
	Password string `form:"stylometry_sample_password" json:"stylometry_sample_password"`
}

type stylometryAPILoginRequest struct {
	Name     string `json:"stylometry_sample_name"`
	Password string `json:"stylometry_sample_password"`
}

// stylometrySplitDraftText separates the author from the text entered in one field.
func stylometrySplitDraftText(stylometry_text string) (string, string, error) {
	stylometry_text = strings.ReplaceAll(stylometry_text, "\r\n", "\n")
	stylometry_text = strings.ReplaceAll(stylometry_text, "\r", "\n")
	stylometry_parts := strings.SplitN(stylometry_text, "\n", 2)
	if len(stylometry_parts) != 2 {
		return "", "", errors.New("поле text должно содержать автора в первой строке и текстовый отрывок ниже")
	}

	stylometry_text_creator := strings.TrimSpace(stylometry_parts[0])
	stylometry_description := stylometry_parts[1]
	if stylometry_text_creator == "" || strings.TrimSpace(stylometry_description) == "" {
		return "", "", errors.New("автор и текстовый отрывок в поле text не должны быть пустыми")
	}

	return stylometry_text_creator, stylometry_description, nil
}

// APIGetSampleList godoc
// @Summary Список опубликованных образцов
// @Description Без фильтра возвращает все опубликованные образцы. Без JWT признаки владельца и лайка равны 0; с действительным JWT вычисляются для вошедшего пользователя.
// @Tags API: образцы
// @Produce json
// @Param stylometry_avg_len query number false "Максимальная средняя длина предложения, от 0 до 30"
// @Success 200 {object} SwaggerSampleListResponse
// @Failure 400 {object} SwaggerErrorResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Router /api/samples [get]
func (stylometry_h *SampleHandler) APIGetSampleList(stylometry_ctx *gin.Context) {
	stylometry_raw_filter := strings.TrimSpace(stylometry_ctx.Query("stylometry_avg_len"))

	var stylometry_max_avg_len *float64
	if stylometry_raw_filter != "" {
		stylometry_num, stylometry_err := stylometryParseFloat(stylometry_raw_filter)
		if stylometry_err != nil || stylometry_num < stylometryAvgLenMin || stylometry_num > stylometryAvgLenMax {
			stylometry_ctx.JSON(http.StatusBadRequest, gin.H{
				"error": "stylometry_avg_len должен быть числом от 0 до 30",
			})
			return
		}
		stylometry_max_avg_len = &stylometry_num
	}

	stylometry_samples, stylometry_err := stylometry_h.Repository.FilteredSamples(stylometry_max_avg_len)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "ошибка базы данных"})
		return
	}

	stylometry_ctx.JSON(http.StatusOK, gin.H{
		"items": serializer.SampleListResponse(stylometry_samples, auth.UserID(stylometry_ctx)),
	})
}

// APIGetSampleFeed godoc
// @Summary Опубликованный образец из ленты
// @Description Без ID возвращает первый образец; с ID возвращает его, а при next=true следующий опубликованный образец.
// @Tags API: образцы
// @Produce json
// @Param stylometry_id query integer false "ID образца"
// @Param next query string false "Строка true для следующего образца; другое значение означает false"
// @Success 200 {object} serializer.SampleResponseDTO
// @Failure 400 {object} SwaggerErrorResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 404 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Router /api/samples/feed [get]
func (stylometry_h *SampleHandler) APIGetSampleFeed(stylometry_ctx *gin.Context) {
	stylometry_raw_id := strings.TrimSpace(stylometry_ctx.Query("stylometry_id"))
	stylometry_next := strings.EqualFold(strings.TrimSpace(stylometry_ctx.Query("next")), "true")

	var stylometry_sample_id *uint
	if stylometry_raw_id != "" {
		stylometry_id, stylometry_err := strconv.ParseUint(stylometry_raw_id, 10, 64)
		if stylometry_err != nil {
			stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "некорректный stylometry_id"})
			return
		}
		stylometry_uint_id := uint(stylometry_id)
		stylometry_sample_id = &stylometry_uint_id
	}

	stylometry_sample, stylometry_ok, stylometry_err := stylometry_h.Repository.FeedSample(stylometry_sample_id, stylometry_next)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "ошибка базы данных"})
		return
	}
	if !stylometry_ok {
		stylometry_ctx.JSON(http.StatusNotFound, gin.H{"error": "опубликованный образец не найден"})
		return
	}

	stylometry_ctx.JSON(http.StatusOK, serializer.SampleResponse(stylometry_sample, auth.UserID(stylometry_ctx)))
}

// APIGetSampleDraft godoc
// @Summary Черновик текущего пользователя
// @Description Возвращает черновик пользователя из JWT. Если черновика нет, draft равен null.
// @Tags API: образцы
// @Produce json
// @Success 200 {object} SwaggerDraftResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Security BearerJWT
// @Router /api/samples/draft [get]
func (stylometry_h *SampleHandler) APIGetSampleDraft(stylometry_ctx *gin.Context) {
	stylometry_draft, stylometry_has_draft, stylometry_err := stylometry_h.Repository.DraftSample(auth.UserID(stylometry_ctx))
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "ошибка базы данных"})
		return
	}
	if !stylometry_has_draft {
		stylometry_ctx.JSON(http.StatusOK, gin.H{
			"has_draft": false,
			"draft":     nil,
		})
		return
	}

	stylometry_ctx.JSON(http.StatusOK, gin.H{
		"has_draft": true,
		"draft":     serializer.SampleResponse(stylometry_draft, auth.UserID(stylometry_ctx)),
	})
}

// APIPostSampleDraft godoc
// @Summary Создать или обновить черновик с изображением и видео
// @Description Первая строка поля text — автор; остальные строки — текст. Файлы загружаются в MinIO. CreatorID определяется по JWT.
// @Tags API: образцы
// @Accept multipart/form-data
// @Produce json
// @Param text formData string true "Автор в первой строке и текстовый отрывок ниже"
// @Param image formData file true "Файл изображения"
// @Param video formData file true "Файл видео"
// @Success 201 {object} SwaggerCreatedDraftResponse
// @Failure 400 {object} SwaggerErrorResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Security BearerJWT
// @Router /api/samples [post]
func (stylometry_h *SampleHandler) APIPostSampleDraft(stylometry_ctx *gin.Context) {
	stylometry_text_creator, stylometry_description, stylometry_err := stylometrySplitDraftText(stylometry_ctx.PostForm("text"))
	if stylometry_err != nil {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": stylometry_err.Error()})
		return
	}

	stylometry_image_file, stylometry_err := stylometryAPIFormFile(stylometry_ctx, "image")
	if errors.Is(stylometry_err, http.ErrMissingFile) {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "нужно приложить файл изображения в поле image"})
		return
	}
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "не удалось прочитать файл изображения"})
		return
	}

	stylometry_video_file, stylometry_err := stylometryAPIFormFile(stylometry_ctx, "video")
	if errors.Is(stylometry_err, http.ErrMissingFile) {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "нужно приложить файл видео в поле video"})
		return
	}
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "не удалось прочитать файл видео"})
		return
	}

	stylometry_image_url, stylometry_err := media.UploadFile(stylometry_ctx.Request.Context(), stylometry_image_file, "stylometry_images")
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить изображение в MinIO"})
		return
	}

	stylometry_video_url, stylometry_err := media.UploadFile(stylometry_ctx.Request.Context(), stylometry_video_file, "stylometry_videos")
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось загрузить видео в MinIO"})
		return
	}

	stylometry_sample, stylometry_err := stylometry_h.Repository.CreateDraft(
		auth.UserID(stylometry_ctx),
		stylometry_text_creator,
		stylometry_description,
		stylometry_image_url,
		stylometry_video_url,
	)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось сохранить черновик"})
		return
	}

	stylometry_ctx.JSON(http.StatusCreated, gin.H{
		"draft": serializer.SampleResponse(stylometry_sample, auth.UserID(stylometry_ctx)),
	})
}

// APIPutSamplePublishDraft godoc
// @Summary Опубликовать черновик текущего пользователя
// @Description Тело не требуется. Показатели текста пересчитываются при публикации своего черновика.
// @Tags API: образцы
// @Produce json
// @Success 200 {object} SwaggerPublishedResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 404 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Security BearerJWT
// @Router /api/samples/draft/publish [put]
func (stylometry_h *SampleHandler) APIPutSamplePublishDraft(stylometry_ctx *gin.Context) {
	stylometry_draft, stylometry_has_draft, stylometry_err := stylometry_h.Repository.DraftSample(auth.UserID(stylometry_ctx))
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "ошибка базы данных"})
		return
	}
	if !stylometry_has_draft {
		stylometry_ctx.JSON(http.StatusNotFound, gin.H{"error": "черновик текущего пользователя не найден"})
		return
	}

	stylometry_metrics := analysis.AnalyzeText(stylometry_draft.ShortDescription)
	stylometry_err = stylometry_h.Repository.PublishDraft(
		auth.UserID(stylometry_ctx),
		stylometry_draft.ShortDescription,
		stylometry_metrics.AverageSentenceLength,
		stylometry_metrics.ConjunctionFrequency,
	)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось опубликовать черновик"})
		return
	}

	stylometry_published, stylometry_ok, stylometry_err := stylometry_h.Repository.SampleByID(stylometry_draft.ID)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "черновик опубликован, но карточку не удалось перечитать"})
		return
	}
	if !stylometry_ok {
		stylometry_ctx.JSON(http.StatusOK, gin.H{"published": true})
		return
	}

	stylometry_ctx.JSON(http.StatusOK, gin.H{
		"published": true,
		"sample":    serializer.SampleResponse(stylometry_published, auth.UserID(stylometry_ctx)),
	})
}

// APIDeleteSample godoc
// @Summary Логически удалить образец
// @Description Удаляет только собственный образец. Для чужого образца возвращает 403, для отсутствующего — 404.
// @Tags API: образцы
// @Produce json
// @Param id path integer true "ID образца"
// @Success 200 {object} SwaggerDeletedResponse
// @Failure 400 {object} SwaggerErrorResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 403 {object} SwaggerErrorResponse
// @Failure 404 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Security BearerJWT
// @Router /api/samples/{id} [delete]
func (stylometry_h *SampleHandler) APIDeleteSample(stylometry_ctx *gin.Context) {
	stylometry_id, stylometry_err := strconv.ParseUint(strings.TrimSpace(stylometry_ctx.Param("id")), 10, 64)
	if stylometry_err != nil {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "некорректный id карточки"})
		return
	}
	stylometry_owner_id, stylometry_found, stylometry_err := stylometry_h.Repository.SampleOwnerID(uint(stylometry_id))
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "ошибка базы данных"})
		return
	}
	if !stylometry_found {
		stylometry_ctx.JSON(http.StatusNotFound, gin.H{"error": "карточка не найдена или уже удалена"})
		return
	}
	if stylometry_owner_id != auth.UserID(stylometry_ctx) {
		stylometry_ctx.JSON(http.StatusForbidden, gin.H{"error": "нельзя удалить чужую карточку"})
		return
	}

	stylometry_err = stylometry_h.Repository.DeleteSampleSQL(auth.UserID(stylometry_ctx), uint(stylometry_id))
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusNotFound, gin.H{"error": "карточка текущего пользователя не найдена или уже удалена"})
		return
	}

	stylometry_ctx.JSON(http.StatusOK, gin.H{"deleted": true})
}

// APIPostSampleLike godoc
// @Summary Поставить или снять лайк
// @Description JSON с stylometry_like или like: 1 ставит лайк, 0 снимает. Также принимается form-data. Лайк принадлежит пользователю из JWT.
// @Tags API: образцы
// @Accept json
// @Produce json
// @Param id path integer true "ID образца"
// @Param request body SwaggerLikeRequest true "Одно из полей stylometry_like или like со значением 0 либо 1"
// @Success 200 {object} SwaggerLikeResponse
// @Failure 400 {object} SwaggerErrorResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 404 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Security BearerJWT
// @Router /api/samples/{id}/like [post]
func (stylometry_h *SampleHandler) APIPostSampleLike(stylometry_ctx *gin.Context) {
	stylometry_id, stylometry_err := strconv.ParseUint(strings.TrimSpace(stylometry_ctx.Param("id")), 10, 64)
	if stylometry_err != nil {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "некорректный id карточки"})
		return
	}

	var stylometry_request stylometryAPILikeRequest
	if stylometry_err = stylometry_ctx.ShouldBind(&stylometry_request); stylometry_err != nil {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "нужно передать stylometry_like: 0 или 1"})
		return
	}

	var stylometry_like_value int
	if stylometry_request.StylometryLike != nil {
		stylometry_like_value = *stylometry_request.StylometryLike
	} else if stylometry_request.Like != nil {
		stylometry_like_value = *stylometry_request.Like
	} else {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "нужно передать stylometry_like: 0 или 1"})
		return
	}

	if stylometry_like_value != 0 && stylometry_like_value != 1 {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "stylometry_like должен быть 0 или 1"})
		return
	}

	stylometry_ok, stylometry_err := stylometry_h.Repository.SetLike(auth.UserID(stylometry_ctx), uint(stylometry_id), stylometry_like_value)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось изменить лайк"})
		return
	}
	if !stylometry_ok {
		stylometry_ctx.JSON(http.StatusNotFound, gin.H{"error": "опубликованная карточка не найдена"})
		return
	}

	stylometry_sample, _, stylometry_err := stylometry_h.Repository.SampleByID(uint(stylometry_id))
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "лайк изменён, но карточку не удалось перечитать"})
		return
	}

	stylometry_ctx.JSON(http.StatusOK, gin.H{
		"liked":  stylometry_like_value == 1,
		"sample": serializer.SampleResponse(stylometry_sample, auth.UserID(stylometry_ctx)),
	})
}

// APIPostUserRegister godoc
// @Summary Зарегистрировать пользователя
// @Description Принимает JSON или form-data с именем и паролем. Пароль сохраняется как bcrypt-хеш и не возвращается.
// @Tags API: пользователи
// @Accept json
// @Produce json
// @Param request body SwaggerRegisterRequest true "Имя и пароль"
// @Success 201 {object} SwaggerRegisteredResponse
// @Failure 400 {object} SwaggerErrorResponse
// @Failure 409 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Router /api/users [post]
func (stylometry_h *SampleHandler) APIPostUserRegister(stylometry_ctx *gin.Context) {
	var stylometry_request stylometryAPIUserRequest
	if stylometry_err := stylometry_ctx.ShouldBind(&stylometry_request); stylometry_err != nil {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "нужно передать данные пользователя"})
		return
	}

	stylometry_name := strings.TrimSpace(stylometry_request.Name)
	stylometry_password := stylometry_request.Password

	if stylometry_name == "" {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "нужно передать stylometry_sample_name"})
		return
	}
	if strings.TrimSpace(stylometry_password) == "" {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "нужно передать stylometry_sample_password",
		})
		return
	}

	stylometry_user, stylometry_err := stylometry_h.Repository.RegisterUser(stylometry_name, stylometry_password)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		if strings.Contains(strings.ToLower(stylometry_err.Error()), "duplicate") {
			stylometry_ctx.JSON(http.StatusConflict, gin.H{"error": "пользователь с таким именем уже существует"})
			return
		}
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось зарегистрировать пользователя"})
		return
	}

	stylometry_ctx.JSON(http.StatusCreated, gin.H{
		"user": serializer.UserResponse(stylometry_user),
	})
}

// APIPostAuthLogin godoc
// @Summary Войти и получить JWT
// @Description Проверяет пароль по bcrypt-хешу и выдаёт подписанный access token на 1 час.
// @Tags API: вход
// @Accept json
// @Produce json
// @Param request body SwaggerLoginRequest true "Имя и пароль"
// @Success 200 {object} SwaggerLoginResponse
// @Failure 400 {object} SwaggerErrorResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 500 {object} SwaggerErrorResponse
// @Router /api/auth/login [post]
func (stylometry_h *SampleHandler) APIPostAuthLogin(stylometry_ctx *gin.Context) {
	var stylometry_request stylometryAPILoginRequest
	if stylometry_err := stylometry_ctx.ShouldBindJSON(&stylometry_request); stylometry_err != nil {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "нужно передать имя и пароль в JSON"})
		return
	}

	stylometry_name := strings.TrimSpace(stylometry_request.Name)
	if stylometry_name == "" || strings.TrimSpace(stylometry_request.Password) == "" {
		stylometry_ctx.JSON(http.StatusBadRequest, gin.H{"error": "имя и пароль обязательны"})
		return
	}

	stylometry_user, stylometry_err := stylometry_h.Repository.UserByName(stylometry_name)
	if errors.Is(stylometry_err, gorm.ErrRecordNotFound) {
		stylometry_ctx.JSON(http.StatusUnauthorized, gin.H{"error": "неверное имя или пароль"})
		return
	}
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "ошибка базы данных"})
		return
	}
	if !password.Check(stylometry_request.Password, stylometry_user.Password) {
		stylometry_ctx.JSON(http.StatusUnauthorized, gin.H{"error": "неверное имя или пароль"})
		return
	}

	stylometry_token, stylometry_err := stylometry_h.TokenIssuer.Issue(stylometry_user.ID)
	if stylometry_err != nil {
		logrus.Error("не удалось выпустить JWT: ", stylometry_err)
		stylometry_ctx.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось выполнить вход"})
		return
	}
	stylometry_session_id, stylometry_err := stylometry_h.Sessions.Create(stylometry_ctx.Request.Context(), stylometry_user.ID)
	if stylometry_err != nil {
		logrus.Error("не удалось создать сессию в Redis: ", stylometry_err)
		stylometry_ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "не удалось создать сессию"})
		return
	}
	http.SetCookie(stylometry_ctx.Writer, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    stylometry_session_id,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	stylometry_ctx.JSON(http.StatusOK, gin.H{
		"access_token": stylometry_token,
		"token_type":   "Bearer",
		"expires_in":   int(auth.AccessTokenTTL.Seconds()),
	})
}

// APIPostAuthLogout godoc
// @Summary Выйти и отозвать JWT
// @Description Сохраняет jti текущего JWT в Redis blacklist до окончания его срока действия. Повторный выход с тем же токеном возвращает 401.
// @Tags API: вход
// @Produce json
// @Success 200 {object} SwaggerLogoutResponse
// @Failure 401 {object} SwaggerErrorResponse
// @Failure 503 {object} SwaggerErrorResponse
// @Security BearerJWT
// @Router /api/auth/logout [post]
func (stylometry_h *SampleHandler) APIPostAuthLogout(stylometry_ctx *gin.Context) {
	if stylometry_session_id, stylometry_has_session := auth.SessionIDFromContext(stylometry_ctx); stylometry_has_session {
		if stylometry_err := stylometry_h.Sessions.Destroy(stylometry_ctx.Request.Context(), stylometry_session_id); stylometry_err != nil {
			stylometry_ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "не удалось удалить сессию из Redis"})
			return
		}
		http.SetCookie(stylometry_ctx.Writer, &http.Cookie{Name: auth.SessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		stylometry_ctx.JSON(http.StatusOK, gin.H{"deauthenticated": true, "stylometry_auth_comment": "sessionId удалён из Redis"})
		return
	}
	stylometry_token, stylometry_ok := auth.VerifiedTokenFromContext(stylometry_ctx)
	if !stylometry_ok {
		stylometry_ctx.JSON(http.StatusUnauthorized, gin.H{"error": "требуется действительный Bearer JWT"})
		return
	}
	stylometry_saved, stylometry_err := stylometry_h.Blacklist.Revoke(stylometry_ctx.Request.Context(), stylometry_token)
	if errors.Is(stylometry_err, auth.ErrTokenExpired) || !stylometry_saved && stylometry_err == nil {
		stylometry_ctx.JSON(http.StatusUnauthorized, gin.H{"error": "требуется действительный Bearer JWT"})
		return
	}
	if stylometry_err != nil {
		stylometry_ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "не удалось отозвать JWT в Redis"})
		return
	}
	stylometry_ctx.JSON(http.StatusOK, gin.H{
		"deauthenticated":         true,
		"stylometry_auth_comment": "JWT отозван до окончания срока действия",
	})
}
