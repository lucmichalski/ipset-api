package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"go_firewall/cmder"
	"regexp"
	"strings"
	"sync"
)

func main() {
	router := gin.Default()

	mapInit()
	//go checkSync()
	router.GET("/membersFromSet", getMembers)
	router.GET("/online-info", getMapInfo)
	router.GET("/group-by-ip", getGroup)
	router.POST("/add", adder)
	router.POST("/del", deleter)
	router.OPTIONS("/*matchAllOptions", corsOptionsAllow)

	router.Run(":9800")
}

func corsOptionsAllow(c *gin.Context) {
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	c.JSON(200, nil)
}

func getMembers(c *gin.Context) {
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	var (
		setName = c.Query("group")
		code    = 0
		cmd     = "ipset list"
		RE      *regexp.Regexp
	)
	text, err := cmder.Exec_shell(cmd)
	switch strings.ToLower(setName) {
	case "weixin":
		RE = regexp.MustCompile(`Name: weixin.*Members:\\n(.*)\\n{\nName}.`)
	case "auth":
		RE = regexp.MustCompile(`Name: weixin.*Members:\\n(.*)\\n{\nName}.`)
	case "permit":
		RE = regexp.MustCompile(`Name: Permit.*Members:\\n(.*?)\\n\\nName: Weixin`)
	}
	if err != nil {
		result := RE.FindStringSubmatch(text)
		var ipList []string
		if len(result) >= 2 {
			ipList = strings.Split(result[1], "\\n")
		} else {
			ipList = []string{}
		}

		c.JSON(200, gin.H{
			"code":    code,
			"group":   setName,
			"members": ipList,
		})
	}
}

func getGroup(c *gin.Context) {
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	ip := c.DefaultQuery("ip", c.ClientIP())
	var (
		group string
	)
	value, ok := dict.Load(ip)
	if ok {
		//在weixin 或 all中
		if value == weixin {
			group = "weixin"
		} else {
			group = "all"
		}
	} else {
		group = "none"
	}
	c.JSON(200, gin.H{
		"code":  0,
		"ip":    ip,
		"group": group,
	})
}

const (
	weixin int8 = 1
	all    int8 = 2
)

//map[string]int8
var dict sync.Map

func mapInit() {
	var (
		//注意[\s\S]才能匹配任意字符，.匹配不到\n换行符
		weixinRE = regexp.MustCompile(`Name: weixin[\s\S]*Members:\n([\s\S]*)\n\nName`)
		allRE    = regexp.MustCompile(`Name: all[\s\S]*Members:\n([\s\S]*)\n\nName`)
	)
	//先判断weixin和all两个组，在服务器上ipset list命令后，所显示的位置。
	//如果有一个组在最后，那么获取该组IP列表的正则表达式不一样。go好像不支持正则表达式(?:)
	groupList, err := cmder.Exec_shell("ipset list|grep Name")
	if err == nil {
		groupList = strings.TrimSpace(groupList)
		split := strings.Split(groupList, "\n")
		if strings.HasSuffix(split[len(split)-1], "weixin") {
			weixinRE = regexp.MustCompile(`Name: weixin[\s\S]*Members:\n([\s\S]*)\n`)
		}
		if strings.HasSuffix(split[len(split)-1], "all") {
			allRE = regexp.MustCompile(`Name: all[\s\S]*Members:\n([\s\S]*)\n`)
		}
	} else {
		panic(err)
		fmt.Printf("初始化失败：%s", groupList)
	}

	//获取weixin和set两个组中的IP列表
	text, err := cmder.Exec_shell("ipset list")
	if err != nil {
		panic(err)
		fmt.Printf("初始化失败：%s", text)
	}
	weixinList := weixinRE.FindStringSubmatch(text)
	allList := allRE.FindStringSubmatch(text)

	var (
		weixinIpList []string
		allIpList    []string
	)

	if len(weixinList) >= 2 {
		weixinIpList = strings.Split(weixinList[1], "\n")
	} else {
		weixinIpList = []string{}
	}

	if len(allList) >= 2 {
		allIpList = strings.Split(allList[1], "\n")
	} else {
		allIpList = []string{}
	}

	//遍历添加两个组中的ip到map中，同步服务器ipset数据，完成初始化
	for _, ip := range weixinIpList {
		dict.Store(ip, weixin)
	}
	for _, ip := range allIpList {
		dict.Store(ip, all)
	}

}

func setMap(ip string, group string) error {
	if group == "weixin" {
		dict.Store(ip, weixin)
		return nil
	} else if group == "all" {
		dict.Store(ip, all)
		return nil
	} else {
		return fmt.Errorf("setMap error：group %q not exist", group)
	}
}

func execAndSetMap(ip, group, action string) error {
	cmd := "ipset " + action + " " + group + " " + ip
	cmdOut, err := cmder.Exec_shell(cmd)
	if err != nil {
		return fmt.Errorf(cmdOut)
	}
	return setMap(ip, group)
}

func execAndDeleteMap(ip, group, action string) error {
	cmd := "ipset " + action + " " + group + " " + ip
	cmdOut, err := cmder.Exec_shell(cmd)
	if err != nil {
		return fmt.Errorf(cmdOut)
	}
	dict.Delete(ip)
	return nil
}

func adder(c *gin.Context) {
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	var (
		ip     = c.DefaultPostForm("ip", c.ClientIP())
		group  = strings.ToLower(c.PostForm("group"))
		resErr string
		code   = 0
	)
	if group != "weixin" && group != "all" {
		code = 1
		resErr = fmt.Errorf("group require weixin or all，got %q", group).Error()
	} else {
		groupName, ok := dict.Load(ip)
		if ok {
			// ok表示ip已经在map中
			if groupName == weixin && group == "all" {
				// 从weixin组到all组，1 从weixin组删除ip 2 添加ip到all组
				if err := execAndSetMap(ip, "weixin", "del"); err != nil {
					code = 1
					resErr = resErr + err.Error()
				} else {
					if err := execAndSetMap(ip, "all", "add"); err != nil {
						code = 1
						resErr = resErr + err.Error()
					}
				}
			}
			if groupName == all && group == "weixin" {
				//从all组到weixin组，1从all组删除ip 2添加ip到weixin组
				if err := execAndSetMap(ip, "all", "del"); err != nil {
					code = 1
					resErr = resErr + err.Error()
				} else {
					if err := execAndSetMap(ip, "weixin", "add"); err != nil {
						code = 1
						resErr = resErr + err.Error()
					}
				}
			}
		} else {
			// ip不在map中，也就是不在任何组中
			if group == "weixin" {
				if err := execAndSetMap(ip, "weixin", "add"); err != nil {
					code = 1
					resErr = resErr + err.Error()
				}
			}
			if group == "all" {
				if err := execAndSetMap(ip, "all", "add"); err != nil {
					code = 1
					resErr = resErr + err.Error()
				}
			}
		}
	}

	c.JSON(200, gin.H{
		"code":  code,
		"err":   resErr,
		"ip":    ip,
		"group": group,
	})
}

func deleter(c *gin.Context) {
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	var (
		ip     = c.DefaultPostForm("ip", c.ClientIP())
		code   = 0
		resErr string
		group  string
	)
	groupName, ok := dict.Load(ip)
	if ok {
		//判断所在group，然后从其组删除，然后在map中删除
		if groupName == weixin {
			group = "weixin"
			if err := execAndDeleteMap(ip, "weixin", "del"); err != nil {
				code = 1
				resErr = resErr + err.Error()
			}
		} else {
			group = "all"
			if err := execAndDeleteMap(ip, "all", "del"); err != nil {
				code = 1
				resErr = resErr + err.Error()
			}
		}
	} else {
		resErr = fmt.Errorf("this ip %s is not exist in weixin or all", ip).Error()
		code = 1
	}

	c.JSON(200, gin.H{
		"code":  code,
		"err":   resErr,
		"ip":    ip,
		"group": group,
	})
}

func getMapInfo(c *gin.Context) {
	var (
		weixinCount int
		allCount    int
	)
	dict.Range(func(key, value interface{}) bool {
		//res += fmt.Sprintf("%s-->%s\n", key, group2str(tmpValue))
		if value.(int8) == 1 {
			weixinCount += 1
		} else {
			allCount += 1
		}
		return true
	})
	c.JSON(200, gin.H{
		"weixinCount": weixinCount,
		"allCount":    allCount,
	})
}
