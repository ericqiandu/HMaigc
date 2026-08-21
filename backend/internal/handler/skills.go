package handler

import (
	"errors"
	"strconv"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterSkillRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/skills/capabilities", func(c *gin.Context) {
		ok(c, gin.H{"capabilities": svc.SkillIntegrationCapabilities()})
	})
	r.GET("/skills/catalog", func(c *gin.Context) {
		userID, err := optionalSkillCatalogUserID(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, err := parsePositiveSkillQuery(c.DefaultQuery("page", "1"), "页码", service.SkillCatalogMaximumPage)
		if err != nil {
			failService(c, err)
			return
		}
		pageSize, err := parsePositiveSkillQuery(c.DefaultQuery("page_size", "12"), "每页数量", 60)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.SkillsCatalog(c.Request.Context(), userID, service.SkillListRequest{
			Page: page, PageSize: pageSize, Search: c.Query("search"), Categories: c.QueryArray("categories"),
		})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
	r.GET("/skills/activated", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		skills, err := svc.ActivatedSkills(c.Request.Context(), user.ID)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"skills": skills})
	})
	r.GET("/skills/favorites", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		skills, err := svc.FavoriteSkills(c.Request.Context(), user.ID)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"skills": skills})
	})
	r.GET("/skills/catalog/:dir", func(c *gin.Context) {
		userID, err := optionalSkillCatalogUserID(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		skill, err := svc.SkillDetail(c.Request.Context(), userID, c.Param("dir"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"skill": skill})
	})
	r.POST("/skills/:dir/activate", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		skill, err := svc.SetSkillActivated(c.Request.Context(), user.ID, c.Param("dir"), true)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"skill": skill})
	})
	r.DELETE("/skills/:dir/activate", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		skill, err := svc.SetSkillActivated(c.Request.Context(), user.ID, c.Param("dir"), false)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"skill": skill})
	})
	r.POST("/skills/:dir/favorite", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		skill, err := svc.SetSkillLiked(c.Request.Context(), user.ID, c.Param("dir"), true)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"skill": skill})
	})
	r.DELETE("/skills/:dir/favorite", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		skill, err := svc.SetSkillLiked(c.Request.Context(), user.ID, c.Param("dir"), false)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"skill": skill})
	})
}

func optionalSkillCatalogUserID(c *gin.Context, svc *service.Service) (string, error) {
	cookie := sessionCookie(c)
	if cookie == "" {
		return "", nil
	}
	user, err := svc.CurrentUser(cookie)
	if err != nil {
		var authError *service.AuthError
		if errors.As(err, &authError) && authError.Status == 401 {
			return "", nil
		}
		return "", err
	}
	return user.ID, nil
}

func parsePositiveSkillQuery(raw string, label string, maximum int) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || (maximum > 0 && value > maximum) {
		return 0, service.BadAuthRequest(label + "无效")
	}
	return value, nil
}
