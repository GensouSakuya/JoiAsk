package controller

import (
	"bytes"
	"io/ioutil"
	"joiask-backend/internal/database"
	"joiask-backend/internal/storage"
	"joiask-backend/pkg/util"
	"path"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm/clause"
)

type QuestionController struct{}

type QuestionRequest struct {
	OrderBy  string `form:"order_by"`
	Order    string `form:"order"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
	TagID    int    `form:"tag_id"`
	Search   string `form:"search"`
	Hide     bool   `form:"hide"`
	Rainbow  bool   `form:"rainbow"`
	Archive  bool   `form:"archive"`
	Publish  bool   `form:"publish"`
}

type QuestionModifyRequest struct {
	TagID     int  `json:"tag_id"`
	IsHide    bool `json:"is_hide"`
	IsRainbow bool `json:"is_rainbow"`
	IsArchive bool `json:"is_archive"`
	IsPublish bool `json:"is_publish"`
}

// Get questions
func (*QuestionController) Get(c *gin.Context) {
	// Prepare for auth
	if sessions.Default(c).Get("authed") == true {
		c.Set("authed", true)
	}
	var questionList []database.Question
	var total int64
	var request QuestionRequest
	if err := c.Bind(&request); err != nil {
		Fail(c, 400, "请求错误")
		return
	}
	var tx = database.DB.Model(&database.Question{}).Preload(clause.Associations).Order(getOrderBy(request.OrderBy) + " " + getOrder(request.Order))
	if getOrderBy(request.OrderBy) != "is_archive" {
		tx = tx.Order("is_archive asc")
	}
	if _, ok := c.GetQuery("tag_id"); ok {
		if request.TagID > 0 {
			tx = tx.Where("tag_id = ?", request.TagID)
		}
	}
	if _, ok := c.GetQuery("search"); ok {
		tx = tx.Where("content like ?", "%"+request.Search+"%")
	}
	if _, ok := c.GetQuery("hide"); ok {
		tx = tx.Where("is_hide = ?", request.Hide)
	}
	if _, ok := c.GetQuery("rainbow"); ok {
		tx = tx.Where("is_rainbow = ?", request.Rainbow)
	}
	if _, ok := c.GetQuery("archive"); ok {
		tx = tx.Where("is_archive = ?", request.Archive)
	}
	if _, ok := c.GetQuery("publish"); ok {
		// Normal users can only see published questions
		log.Debug("publish: ", request.Publish)
		if !c.GetBool("authed") {
			tx = tx.Where("is_publish = ?", true)
		} else {
			tx = tx.Where("is_publish = ?", request.Publish)
		}
	} else {
		// Normal users can only see published questions
		if !c.GetBool("authed") {
			tx = tx.Where("is_publish = ?", true)
		}
	}
	err := tx.Count(&total).Scopes(paginate(getPage(request.Page), getPageSize(request.PageSize))).Find(&questionList).Error
	if err != nil {
		log.Error(err)
		Fail(c, 500, "获取提问失败")
		return
	}
	Success(c, gin.H{
		"questions": questionList,
		"total":     total,
		"page":      getPage(request.Page),
		"page_size": getPageSize(request.PageSize),
	})
}

func (*QuestionController) Put(c *gin.Context) {
	var request QuestionModifyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, 400, "请求错误")
		return
	}
	var q database.Question
	database.DB.First(&q, c.Param("id"))
	if q.ID == 0 {
		Fail(c, 404, "提问不存在")
		return
	}
	if request.TagID == 0 {
		Fail(c, 400, "请求错误")
		return
	}
	q.TagID = request.TagID
	q.IsHide = request.IsHide
	q.IsRainbow = request.IsRainbow
	q.IsArchive = request.IsArchive
	q.IsPublish = request.IsPublish
	err := database.DB.Save(&q).Error
	if err != nil {
		log.Error(err)
		Fail(c, 500, "修改提问失败")
		return
	}
	Success(c, nil)
}

func (*QuestionController) Post(c *gin.Context) {
	var tag database.Tag
	tagID := c.PostForm("tag_id")
	database.DB.First(&tag, tagID)
	if tag.ID == 0 {
		Fail(c, 404, "话题不存在")
		return
	}
	var q database.Question
	q.TagID = int(tag.ID)
	q.Content = strings.Trim(c.PostForm("content"), " \r\n\t")
	q.IsHide = c.PostForm("hide") == "true"
	q.IsRainbow = c.PostForm("rainbow") == "true"
	mp, _ := c.MultipartForm()
	for _, v := range mp.File["files[]"] {
		f, err := v.Open()
		if err != nil {
			log.Error(err)
			Fail(c, 405, "文件上传失败")
			return
		}
		fileContent, err := ioutil.ReadAll(f)
		if err != nil {
			log.Error(err)
			Fail(c, 405, "文件上传失败")
			return
		}
		newFileName := util.Md5v(string(fileContent)) + path.Ext(v.Filename)
		url, err := storage.Get().Upload(newFileName, bytes.NewReader(fileContent))
		if err != nil {
			log.Error(err)
			Fail(c, 500, "文件上传失败")
			return
		}
		q.ImagesNum++
		if q.Images == "" {
			q.Images = url
		} else {
			q.Images += ";" + url
		}
	}
	err := database.DB.Save(&q).Error
	if err != nil {
		log.Error(err)
		Fail(c, 500, "创建提问失败")
		return
	}
	Success(c, nil)
}

func (*QuestionController) Delete(c *gin.Context) {
	var q database.Question
	id := c.Param("id")
	err := database.DB.First(&q, id).Error
	if q.ID == 0 {
		log.Error(err)
		Fail(c, 404, "提问不存在")
		return
	}
	err = database.DB.Delete(&q).Error
	if err != nil {
		log.Error(err)
		Fail(c, 500, "删除提问失败")
		return
	}
	Success(c, nil)
}

func (*QuestionController) Like(c *gin.Context) {
	ip := c.ClientIP()
	id, _ := strconv.Atoi(c.Param("id"))
	var lr database.LikeRecord
	database.DB.Where("ip = ? and question_id = ?", ip, id).First(&lr)
	if lr.ID > 0 {
		Fail(c, 400, "您已经点过赞了")
		return
	}
	lr.IP = ip
	lr.QuestionID = id
	tx := database.DB.Begin()
	err := tx.Create(&lr).Error
	if err != nil {
		log.Error(err)
		Fail(c, 500, "点赞失败")
		tx.Rollback()
		return
	}
	// update likes
	var q database.Question
	err = database.DB.Where("id = ?", id).First(&q).Error
	if err != nil {
		log.Error(err)
		Fail(c, 500, "点赞失败")
		tx.Rollback()
		return
	}
	q.Likes++
	err = tx.Save(&q).Error
	if err != nil {
		log.Error(err)
		Fail(c, 500, "点赞失败")
		tx.Rollback()
		return
	}
	if tx.Commit().Error != nil {
		log.Error(err)
		Fail(c, 500, "点赞失败")
		tx.Rollback()
		return
	}
	Success(c, nil)
}
