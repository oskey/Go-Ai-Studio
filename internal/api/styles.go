package api

import (
	"fmt"
	"net/http"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
)

// ListArtStyles retrieves all art styles
func ListArtStyles(c *gin.Context) {
	var styles []models.ArtStyle
	if err := db.DB.Find(&styles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch art styles"})
		return
	}
	c.JSON(http.StatusOK, styles)
}

// AddArtStyle adds a new art style
func AddArtStyle(c *gin.Context) {
	var style models.ArtStyle
	if err := c.ShouldBindJSON(&style); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	style.CreatedAt = time.Now()
	style.UpdatedAt = time.Now()

	if err := db.DB.Create(&style).Error; err != nil {
		Log(LogLevelError, "创建画风失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create art style"})
		return
	}

	Log(LogLevelInfo, "创建画风", fmt.Sprintf("创建了新画风: %s", style.Name))
	c.JSON(http.StatusCreated, style)
}

// UpdateArtStyle updates an existing art style
func UpdateArtStyle(c *gin.Context) {
	id := c.Param("id")
	var style models.ArtStyle
	if err := db.DB.First(&style, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Art style not found"})
		return
	}

	var updateData models.ArtStyle
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	style.Name = updateData.Name
	style.Description = updateData.Description
	style.UpdatedAt = time.Now()

	if err := db.DB.Save(&style).Error; err != nil {
		Log(LogLevelError, "更新画风失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update art style"})
		return
	}

	Log(LogLevelInfo, "更新画风", fmt.Sprintf("更新了画风: %s", style.Name))
	c.JSON(http.StatusOK, style)
}

// DeleteArtStyle deletes an art style
func DeleteArtStyle(c *gin.Context) {
	id := c.Param("id")
	if err := db.DB.Delete(&models.ArtStyle{}, id).Error; err != nil {
		Log(LogLevelError, "删除画风失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete art style"})
		return
	}

	Log(LogLevelInfo, "删除画风", fmt.Sprintf("删除画风 ID: %s", id))
	c.JSON(http.StatusOK, gin.H{"message": "Art style deleted successfully"})
}

// InitDefaultArtStyles populates the database with default styles if empty
// This is called from InitDB or manually via API
func InitDefaultArtStyles() {
	var count int64
	db.DB.Model(&models.ArtStyle{}).Count(&count)
	if count > 0 {
		return
	}

	defaultStyles := []models.ArtStyle{
		// 1. 国风写实
		{
			Name:        "国风仙侠 · 写实",
			Description: "国风仙侠题材，偏写实风格，整体画面真实自然，人物比例符合真实人体结构，皮肤质感自然，古风服饰具有真实布料纹理，颜色克制不浮夸，光影柔和真实，画面统一稳定，非卡通、非动漫风，整体风格严肃、耐看、偏影视感。",
		},
		// 2. 赛博朋克
		{
			Name:        "赛博朋克 · 霓虹",
			Description: "高对比度霓虹色彩，雨夜街道，未来科技感，机械义肢，全息投影，反乌托邦氛围。",
		},
		// 3. 日本动漫 - 宫崎骏风
		{
			Name:        "日漫 · 宫崎骏风",
			Description: "清新治愈，水彩质感，蓝天白云，绿草如茵，充满童真与幻想，色彩明亮柔和。",
		},
		// 4. 日本动漫 - 新海诚风
		{
			Name:        "日漫 · 新海诚风",
			Description: "极致的光影渲染，唯美写实背景，星空、云层、光晕，每一帧都是壁纸。",
		},
		// 5. 韩国网漫 - 现代都市
		{
			Name:        "韩漫 · 现代都市",
			Description: "人物美型，身材比例修长，时尚穿搭，线条干净利落，色彩鲜艳且通透，典型的Webtoon风格。",
		},
		// 6. 韩国网漫 - 奇幻宫廷
		{
			Name:        "韩漫 · 奇幻宫廷",
			Description: "华丽的欧式宫廷或架空奇幻背景，服饰繁复精美，珠宝首饰闪耀，人物贵气逼人。",
		},
		// 7. 中国动漫 - 水墨风
		{
			Name:        "国漫 · 水墨风",
			Description: "传统水墨晕染，留白意境，线条飘逸，色彩淡雅，具有浓厚的中国传统文化底蕴。",
		},
		// 8. 3D 迪士尼/皮克斯风格
		{
			Name:        "3D · 迪士尼/皮克斯",
			Description: "3D渲染，夸张的人物比例（大眼），表情丰富，材质质感细腻（毛发、布料），色彩鲜艳温暖。",
		},
		// 9. 蒸汽朋克
		{
			Name:        "蒸汽朋克 · 复古",
			Description: "维多利亚时代背景，齿轮、蒸汽机、黄铜、皮革，复古机械美学。",
		},
		// 10. 像素艺术
		{
			Name:        "像素艺术 · 8bit",
			Description: "复古游戏风格，明显的像素颗粒，色彩限制，怀旧感。",
		},
		// 11-50. 更多风格 (简化生成，实际项目中可逐一精细配置)
	}

	// 补充更多风格以达到50个
	moreStyles := []models.ArtStyle{
		{Name: "日漫 · 赛璐珞", Description: "经典日本动画上色风格，阴影边界分明，色彩鲜艳。"},
		{Name: "日漫 · 昭和复古", Description: "80-90年代日本动画风格，线条粗犷，色调偏暖。"},
		{Name: "美漫 · 超级英雄", Description: "线条硬朗，肌肉线条夸张，高对比度阴影，动态感强。"},
		{Name: "概念艺术 · 奇幻", Description: "宏大的场景，史诗感，油画质感，用于游戏或电影概念设计。"},
		{Name: "概念艺术 · 科幻", Description: "未来科技，飞船，外星景观，冷色调，硬表面建模感。"},
		{Name: "印象派油画", Description: "模仿莫奈等大师风格，强调光色变化，笔触明显。"},
		{Name: "超现实主义", Description: "梦境般的场景，不合逻辑的组合，达利风格。"},
		{Name: "浮世绘", Description: "日本传统版画风格，线条流畅，平面化构图。"},
		{Name: "低多边形 (Low Poly)", Description: "几何面块构成，简约，折纸感。"},
		{Name: "剪纸艺术", Description: "层叠的纸张效果，阴影投射，平面立体感。"},
		{Name: "黏土定格动画", Description: "黏土材质，手工痕迹，定格动画的质感。"},
		{Name: "特摄片风格", Description: "皮套质感，微缩模型城市，爆炸特效。"},
		{Name: "故障艺术 (Glitch)", Description: "数字信号干扰，色差，像素错位。"},
		{Name: "包豪斯设计", Description: "几何图形，极简主义，红黄蓝三原色。"},
		{Name: "波普艺术", Description: "安迪·沃霍尔风格，重复，高饱和度，网点。"},
		{Name: "中国工笔画", Description: "线条细腻，设色艳丽沉着，写实逼真。"},
		{Name: "敦煌壁画", Description: "古朴，线条流畅，矿物颜料色彩，飞天形象。"},
		{Name: "黑白胶片摄影", Description: "高颗粒感，高对比度，经典，纪实。"},
		{Name: "LOMO 摄影", Description: "暗角，高饱和度，色彩偏移，随性。"},
		{Name: "宝丽来 (Polaroid)", Description: "褪色感，特定边框，柔和，怀旧。"},
		{Name: "微距摄影", Description: "极浅景深，细节惊人，微观世界。"},
		{Name: "移轴摄影", Description: "小人国效果，俯视，上下模糊。"},
		{Name: "红外摄影", Description: "梦幻色彩，白色树叶，非现实光谱。"},
		{Name: "哥特黑暗", Description: "阴郁，黑色，蕾丝，古堡，吸血鬼美学。"},
		{Name: "巴洛克", Description: "宏伟，动态，戏剧性光影，繁复装饰。"},
		{Name: "洛可可", Description: "轻快，优美，粉嫩色彩，不对称构图。"},
		{Name: "极简主义 (Minimalism)", Description: "少即是多，留白，纯净。"},
		{Name: "野兽派", Description: "色彩狂野，非写实颜色，笔触粗犷。"},
		{Name: "立体主义", Description: "破碎，多视角，几何重构。"},
		{Name: "达达主义", Description: "反艺术，拼贴，荒诞，随机。"},
		{Name: "浮雕艺术", Description: "浅浮雕效果，材质感，立体光影。"},
		{Name: "彩色铅笔画", Description: "细腻的笔触，色彩柔和，纸张纹理。"},
		{Name: "粉笔画", Description: "粉尘质感，黑板背景，粗糙线条。"},
		{Name: "马克笔手绘", Description: "硬朗线条，色彩叠加，设计草图感。"},
		{Name: "蜡笔画", Description: "稚拙，厚重色彩，肌理感。"},
		{Name: "涂鸦艺术 (Graffiti)", Description: "街头风格，喷漆质感，夸张字体。"},
		{Name: "矢量扁平 (Flat)", Description: "无阴影，纯色块，UI插画风格。"},
		{Name: "孟菲斯风格 (Memphis)", Description: "几何图案，波点，高饱和度撞色。"},
		{Name: "酸性设计 (Acid)", Description: "液态金属，激光光盘，复古未来，迷幻。"},
		{Name: "Y2K 美学", Description: "千禧年，透明材质，亮粉色，科技乐观主义。"},
		{Name: "新中式 · 国潮", Description: "结合传统中国元素与现代设计，色彩鲜艳，潮流感。"},
		{Name: "日漫 · 少女漫", Description: "眼睛大而闪亮，花朵背景，柔光，浪漫氛围。"},
		{Name: "日漫 · 热血少年", Description: "线条粗犷有力，动态模糊，强调打击感与速度线。"},
		{Name: "韩漫 · 恶女重生", Description: "华丽复杂的欧式长裙，眼神犀利，复仇与权谋氛围。"},
		{Name: "韩漫 · 校园纯爱", Description: "校服，清新的色调，柔和的光影，青春感。"},
		{Name: "美漫 · 黑色电影 (Noir)", Description: "黑白高对比，硬阴影，侦探，雨夜，压抑。"},
		{Name: "绘本插画", Description: "温馨，手绘质感，色彩温暖，适合儿童读物。"},
		{Name: "国漫Q版 · IP锁角稳定", Description: "国漫Q版动画风格，圆润干净线条，明亮统一光色，Q版角色比例稳定，脸型稳定，发型稳定，五官比例稳定，主副配色稳定，服装轮廓稳定，鞋靴造型稳定，材质简洁统一，色彩鲜明通透，适合固定主角、文旅IP、品牌吉祥物和儿童向卡通角色的连续复现。"},
		{Name: "纸片拼贴 (Collage)", Description: "不同材质的纸张剪贴，肌理丰富，边缘撕裂感。"},
		{Name: "体素艺术 (Voxel)", Description: "3D像素，类似我的世界，立方体堆砌。"},
		{Name: "极繁主义 (Maximalism)", Description: "填满画面，色彩爆炸，细节极其丰富，无留白。"},
	}

	defaultStyles = append(defaultStyles, moreStyles...)

	// 使用事务批量插入，提高性能并保证原子性
	tx := db.DB.Begin()
	for _, style := range defaultStyles {
		style.CreatedAt = time.Now()
		style.UpdatedAt = time.Now()
		if err := tx.Create(&style).Error; err != nil {
			tx.Rollback()
			Log(LogLevelError, "初始化画风失败", fmt.Sprintf("插入画风 %s 失败: %v", style.Name, err))
			return
		}
	}
	tx.Commit()

	Log(LogLevelInfo, "初始化画风", fmt.Sprintf("系统启动时自动初始化了 %d 个默认画风", len(defaultStyles)))
}
