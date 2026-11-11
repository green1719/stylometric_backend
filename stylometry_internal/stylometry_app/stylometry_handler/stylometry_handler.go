package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"stylometry/stylometry_internal/stylometry_app/stylometry_analysis"
	"stylometry/stylometry_internal/stylometry_app/stylometry_auth"
	"stylometry/stylometry_internal/stylometry_app/stylometry_model"
	"stylometry/stylometry_internal/stylometry_app/stylometry_repository"
)

// Границы ползунка фильтрации по средней длине предложения.
const (
	stylometryAvgLenMin  = 0.0
	stylometryAvgLenMax  = 30.0
	stylometryAvgLenStep = 0.5
)

// SampleHandler хранит зависимости HTTP-слоя.
type SampleHandler struct {
	Repository  *repository.Repository
	TokenIssuer *auth.Issuer
	Blacklist   auth.Blacklist
	Sessions    auth.SessionStore
}

// NewSampleHandler создаёт HTTP-контроллер.
func NewSampleHandler(stylometry_r *repository.Repository, stylometry_issuer *auth.Issuer, stylometry_blacklist auth.Blacklist, stylometry_sessions auth.SessionStore) *SampleHandler {
	return &SampleHandler{Repository: stylometry_r, TokenIssuer: stylometry_issuer, Blacklist: stylometry_blacklist, Sessions: stylometry_sessions}
}

// stylometryFormatLen красиво форматирует float64 для HTML.
func stylometryFormatLen(stylometry_value float64) string {
	return strconv.FormatFloat(stylometry_value, 'f', -1, 64)
}

// stylometryParseFloat читает число из формы.
func stylometryParseFloat(stylometry_raw string) (float64, error) {
	stylometry_normalized := strings.ReplaceAll(strings.TrimSpace(stylometry_raw), ",", ".")
	return strconv.ParseFloat(stylometry_normalized, 64)
}

// RedirectToSampleFeed godoc
// @Summary Перейти к HTML-ленте
// @Description Доступен только при ENABLE_FRONTEND=true; по умолчанию маршрут не зарегистрирован.
// @Tags HTML: отключено по умолчанию
// @Success 302 {string} string "Переход на /samples/feed"
// @Router /samples [get]
func (stylometry_h *SampleHandler) RedirectToSampleFeed(stylometry_ctx *gin.Context) {
	stylometry_ctx.Redirect(http.StatusFound, "/samples/feed")
}

// GetSampleFeed godoc
// @Summary HTML-страница ленты
// @Description Доступна только при ENABLE_FRONTEND=true; по умолчанию маршрут не зарегистрирован.
// @Tags HTML: отключено по умолчанию
// @Produce html
// @Param stylometry_id query integer false "ID образца"
// @Param next query string false "Строка true для следующего образца"
// @Success 200 {string} string "HTML-страница"
// @Failure 400 {string} string
// @Failure 404 {string} string
// @Failure 500 {string} string
// @Router /samples/feed [get]
func (stylometry_h *SampleHandler) GetSampleFeed(stylometry_ctx *gin.Context) {
	stylometry_raw_id := strings.TrimSpace(stylometry_ctx.Query("stylometry_id"))
	stylometry_next := strings.EqualFold(strings.TrimSpace(stylometry_ctx.Query("next")), "true")
	var stylometry_sample_id *uint

	if stylometry_raw_id != "" {
		stylometry_id, stylometry_err := strconv.ParseUint(stylometry_raw_id, 10, 64)
		if stylometry_err != nil {
			stylometry_ctx.String(http.StatusBadRequest, "некорректный id")
			return
		}
		stylometry_uint_id := uint(stylometry_id)
		stylometry_sample_id = &stylometry_uint_id
	}

	stylometry_sample, stylometry_ok, stylometry_err := stylometry_h.Repository.FeedSample(stylometry_sample_id, stylometry_next)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.String(http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !stylometry_ok {
		stylometry_ctx.String(http.StatusNotFound, "нет опубликованного образца для этого запроса")
		return
	}

	stylometry_ctx.HTML(http.StatusOK, "stylometry_feed.html", gin.H{"stylometry_sample": stylometry_sample})
}

// GetSampleDraft godoc
// @Summary HTML-страница черновика
// @Description Доступна только при ENABLE_FRONTEND=true; по умолчанию маршрут не зарегистрирован.
// @Tags HTML: отключено по умолчанию
// @Produce html
// @Success 200 {string} string "HTML-страница"
// @Failure 500 {string} string
// @Router /samples/draft [get]
func (stylometry_h *SampleHandler) GetSampleDraft(stylometry_ctx *gin.Context) {
	stylometry_draft, stylometry_has_draft, stylometry_err := stylometry_h.Repository.DraftSample(auth.UserID(stylometry_ctx))
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.String(http.StatusInternalServerError, "ошибка базы данных")
		return
	}

	if !stylometry_has_draft {
		stylometry_draft = model.Sample{
			TextCreator:      "",
			ImageURL:         repository.DefaultImageURL,
			VideoURL:         repository.DefaultVideoURL,
			Status:           model.StatusDraft,
			ShortDescription: "",
		}
	}

	stylometry_ctx.HTML(http.StatusOK, "stylometry_draft.html", gin.H{
		"stylometry_sample":    stylometry_draft,
		"stylometry_has_draft": stylometry_has_draft,
	})
}

// PostSampleCreateDraft godoc
// @Summary Создать черновик из HTML-формы
// @Description Доступен только при ENABLE_FRONTEND=true; по умолчанию маршрут не зарегистрирован. После успеха перенаправляет на страницу черновика.
// @Tags HTML: отключено по умолчанию
// @Accept application/x-www-form-urlencoded
// @Param stylometry_text_creator formData string false "Автор текста; также принимается stylometry_title"
// @Param stylometry_title formData string false "Резервное поле автора"
// @Param stylometry_descr formData string false "Текстовый отрывок; также принимается stylometry_sample_descr"
// @Param stylometry_sample_descr formData string false "Резервное поле текста"
// @Param stylometry_image_url formData string false "URL изображения"
// @Param stylometry_video_url formData string false "URL видео"
// @Success 303 {string} string "Переход на /samples/draft"
// @Failure 400 {string} string
// @Failure 500 {string} string
// @Router /samples/draft [post]
func (stylometry_h *SampleHandler) PostSampleCreateDraft(stylometry_ctx *gin.Context) {
	stylometry_text_creator := strings.TrimSpace(stylometry_ctx.PostForm("stylometry_text_creator"))
	if stylometry_text_creator == "" {
		stylometry_text_creator = strings.TrimSpace(stylometry_ctx.PostForm("stylometry_title"))
	}
	if stylometry_text_creator == "" {
		stylometry_text_creator = "Новый автор"
	}

	stylometry_description := strings.TrimSpace(stylometry_ctx.PostForm("stylometry_descr"))
	if stylometry_description == "" {
		stylometry_description = strings.TrimSpace(stylometry_ctx.PostForm("stylometry_sample_descr"))
	}
	if stylometry_description == "" {
		stylometry_ctx.String(http.StatusBadRequest, "текстовый отрывок не заполнен")
		return
	}
	stylometry_image_url := strings.TrimSpace(stylometry_ctx.PostForm("stylometry_image_url"))
	stylometry_video_url := strings.TrimSpace(stylometry_ctx.PostForm("stylometry_video_url"))

	_, stylometry_err := stylometry_h.Repository.CreateDraft(auth.UserID(stylometry_ctx), stylometry_text_creator, stylometry_description, stylometry_image_url, stylometry_video_url)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.String(http.StatusInternalServerError, "не удалось создать черновик")
		return
	}

	stylometry_ctx.Redirect(http.StatusSeeOther, "/samples/draft")
}

// PostSamplePublishDraft godoc
// @Summary Опубликовать черновик из HTML-формы
// @Description Доступен только при ENABLE_FRONTEND=true; по умолчанию маршрут не зарегистрирован. После успеха перенаправляет на плитку.
// @Tags HTML: отключено по умолчанию
// @Accept application/x-www-form-urlencoded
// @Param stylometry_descr formData string false "Текстовый отрывок для публикации"
// @Success 303 {string} string "Переход на /samples/grid"
// @Failure 500 {string} string
// @Router /samples/draft/publish [post]
func (stylometry_h *SampleHandler) PostSamplePublishDraft(stylometry_ctx *gin.Context) {
	stylometry_description := strings.TrimSpace(stylometry_ctx.PostForm("stylometry_descr"))
	stylometry_metrics := analysis.AnalyzeText(stylometry_description)

	stylometry_err := stylometry_h.Repository.PublishDraft(
		auth.UserID(stylometry_ctx),
		stylometry_description,
		stylometry_metrics.AverageSentenceLength,
		stylometry_metrics.ConjunctionFrequency,
	)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.String(http.StatusInternalServerError, "не удалось опубликовать черновик")
		return
	}

	stylometry_ctx.Redirect(http.StatusSeeOther, "/samples/grid")
}

// PostSampleDelete godoc
// @Summary Логически удалить образец из HTML-формы
// @Description Доступен только при ENABLE_FRONTEND=true; по умолчанию маршрут не зарегистрирован. После успеха перенаправляет на плитку.
// @Tags HTML: отключено по умолчанию
// @Accept application/x-www-form-urlencoded
// @Param stylometry_id formData integer true "ID образца"
// @Success 303 {string} string "Переход на /samples/grid"
// @Failure 400 {string} string
// @Failure 500 {string} string
// @Router /samples/delete [post]
func (stylometry_h *SampleHandler) PostSampleDelete(stylometry_ctx *gin.Context) {
	stylometry_raw_id := strings.TrimSpace(stylometry_ctx.PostForm("stylometry_id"))
	stylometry_id, stylometry_err := strconv.ParseUint(stylometry_raw_id, 10, 64)
	if stylometry_err != nil {
		stylometry_ctx.String(http.StatusBadRequest, "некорректный id")
		return
	}

	stylometry_err = stylometry_h.Repository.DeleteSampleSQL(auth.UserID(stylometry_ctx), uint(stylometry_id))
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.String(http.StatusInternalServerError, "не удалось удалить карточку")
		return
	}

	stylometry_ctx.Redirect(http.StatusSeeOther, "/samples/grid")
}

// GetSampleGrid godoc
// @Summary HTML-страница плитки образцов
// @Description Доступна только при ENABLE_FRONTEND=true; по умолчанию маршрут не зарегистрирован.
// @Tags HTML: отключено по умолчанию
// @Produce html
// @Param stylometry_avg_len query number false "Максимальная средняя длина предложения, от 0 до 30"
// @Success 200 {string} string "HTML-страница"
// @Failure 400 {string} string
// @Failure 500 {string} string
// @Router /samples/grid [get]
func (stylometry_h *SampleHandler) GetSampleGrid(stylometry_ctx *gin.Context) {
	stylometry_raw_filter := strings.TrimSpace(stylometry_ctx.Query("stylometry_avg_len"))

	var stylometry_max_avg_len *float64
	if stylometry_raw_filter != "" {
		stylometry_num, stylometry_err := stylometryParseFloat(stylometry_raw_filter)
		if stylometry_err != nil || stylometry_num < stylometryAvgLenMin || stylometry_num > stylometryAvgLenMax {
			stylometry_ctx.String(http.StatusBadRequest, "stylometry_avg_len должен быть числом от %s до %s", stylometryFormatLen(stylometryAvgLenMin), stylometryFormatLen(stylometryAvgLenMax))
			return
		}
		stylometry_max_avg_len = &stylometry_num
	}

	stylometry_samples, stylometry_err := stylometry_h.Repository.FilteredSamples(stylometry_max_avg_len)
	if stylometry_err != nil {
		logrus.Error(stylometry_err)
		stylometry_ctx.String(http.StatusInternalServerError, "ошибка базы данных")
		return
	}

	stylometry_cover_url := repository.DefaultImageURL
	if len(stylometry_samples) > 0 {
		stylometry_cover_url = stylometry_samples[0].ImageURL
	}

	stylometry_slider_value := stylometryFormatLen(stylometryAvgLenMax)
	if stylometry_max_avg_len != nil {
		stylometry_slider_value = stylometryFormatLen(*stylometry_max_avg_len)
	}

	stylometry_ctx.HTML(http.StatusOK, "stylometry_grid.html", gin.H{
		"has_stylometry_samples":   len(stylometry_samples) > 0,
		"stylometry_samples":       stylometry_samples,
		"stylometry_avg_len":       stylometry_raw_filter,
		"stylometry_filter_on":     stylometry_max_avg_len != nil,
		"stylometry_cover_url":     stylometry_cover_url,
		"stylometry_avg_len_value": stylometry_slider_value,
		"stylometry_avg_len_min":   stylometryFormatLen(stylometryAvgLenMin),
		"stylometry_avg_len_max":   stylometryFormatLen(stylometryAvgLenMax),
		"stylometry_avg_len_step":  stylometryFormatLen(stylometryAvgLenStep),
	})
}
