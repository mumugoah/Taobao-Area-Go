package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"regexp"

	"fmt"

	"strings"

	"os"

	"github.com/gocarina/gocsv"
	"github.com/tidwall/gjson"
	"sync"
)

const jsUrl = "https://g.alicdn.com/vip/address/6.0.14/index-min.js"

type Area struct {
	ID       int
	Name     string
	TraName  string
	EnName   string
	ParentID int
	typeID   int
	classID  int
}

type StreetDownloadInfo struct {
	provinceID int
	cityID     int
	areaID     int
}

var csvWriter *gocsv.SafeCSVWriter
var wg *sync.WaitGroup

func main() {

	file, err := os.OpenFile("tmp/address3.csv", os.O_RDWR|os.O_CREATE, os.ModePerm)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	csvWriter = gocsv.DefaultCSVWriter(file)
	wg = &sync.WaitGroup{}
	//下载文件
	s := fetch(jsUrl)
	//获取地址
	areas := getData(s)
	sInfo := make(chan StreetDownloadInfo)

	//设置下载器
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go getStreet(sInfo)
	}

	//获取街道
	getStreets(areas, sInfo)

	wg.Wait()

}

func fetch(url string) []byte {
	res, err := http.Get(url)
	if err != nil {
		log.Printf("fetch error: %s", err)
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Printf("fetch result read error: %s", err)
	}
	return body
}

func getData(s []byte) []Area {
	re1 := regexp.MustCompile(`\[\[[^{}]+?\]\]`)
	re2 := regexp.MustCompile(`\[\[[^{}]+?\]\]\]`)
	datas1 := re1.FindAll(s, -1)
	datas2 := re2.FindAll(s, -1)
	datas := append(datas1, datas2...)
	var areas []Area

	for i, data := range datas {
		json := gjson.ParseBytes(data)
		if i < 4 {
			//省
			for _, pJson := range json.Array() {
				area := Area{}
				area.ID = int(pJson.Get("0").Int())
				area.Name = pJson.Get("1.0").String()
				area.TraName = pJson.Get("1.1").String()
				area.ParentID = int(pJson.Get("2").Int())
				area.classID = 1
				areas = append(areas, area)
			}
		} else if i < 7 {
			//市/区/县
			for _, pJson := range json.Array() {
				area := Area{}
				area.ID = int(pJson.Get("0").Int())
				area.Name = pJson.Get("1.0").String()
				area.TraName = pJson.Get("1.1").String()
				area.ParentID = int(pJson.Get("2").Int())
				area.typeID = int(pJson.Get("3").Int())
				area.classID = 2
				areas = append(areas, area)
			}
		} else if i < 8 {
			// 国外城市
			for _, pJson := range json.Array() {
				area := Area{}
				area.ID = int(pJson.Get("0").Int())
				area.Name = pJson.Get("1.0").String()
				area.TraName = pJson.Get("1.1").String()
				area.EnName = pJson.Get("1.2").String()
				area.ParentID = int(pJson.Get("2").Int())
				area.classID = 5
				areas = append(areas, area)
			}
		} else {
			// 国家
			for _, pJson := range json.Array() {
				if i == 9 {
					continue
				}
				area := Area{}
				area.ID = int(pJson.Get("0").Int())
				area.Name = pJson.Get("1.0").String()
				area.TraName = pJson.Get("1.1").String()
				area.EnName = pJson.Get("1.2").String()
				area.classID = 0
				areas = append(areas, area)
			}
		}
	}
	//添加中国
	china := Area{ID: 1, Name: "中国", TraName: "中國", EnName: "China", ParentID: 0, typeID: 0, classID: 0}
	areas = append(areas, china)

	err := gocsv.MarshalCSV(&areas, csvWriter)
	if err != nil {
		log.Printf("error: %s", err)
	}
	csvWriter.Flush()
	return areas
}

func getStreets(areas []Area, sdc chan StreetDownloadInfo) {
	for _, area := range areas {
		//如果是国内省
		if area.classID == 1 {
			//获取所有下属市
			for _, area2 := range areas {
				if area.ID == area2.ParentID {
					//只获取所有下属区
					for _, area3 := range areas {
						if area2.ID == area3.ParentID && area3.typeID == 0 {
							//打印
							////获取街道
							sdc <- StreetDownloadInfo{area.ID, area2.ID, area3.ID}
						}
					}
				}
			}
		}
	}

}

func getStreet(sdc chan StreetDownloadInfo) {
	for sd := range sdc {
		urlS := fmt.Sprintf("https://lsp.wuliu.taobao.com/locationservice/addr/output_address_town_array.do?l1=%d&l2=%d&l3=%d&lang=zh-S", sd.provinceID, sd.cityID, sd.areaID)
		contentS := fetch(urlS)
		contentT := fetch(fmt.Sprintf("https://lsp.wuliu.taobao.com/locationservice/addr/output_address_town_array.do?l1=%d&l2=%d&l3=%d&lang=zh-T", sd.provinceID, sd.cityID, sd.areaID))
		re := regexp.MustCompile(`\[\[[^{}]+?\]\]`)
		dataS := re.Find(contentS)
		dataT := re.Find(contentT)
		// 替换''
		dataSS := strings.Replace(string(dataS), "'", "\"", -1)
		dataTS := strings.Replace(string(dataT), "'", "\"", -1)
		jsonS := gjson.Parse(dataSS)
		jsonT := gjson.Parse(dataTS)

		var areas []Area
		for i, street := range jsonS.Array() {
			area := Area{}
			area.ID = int(street.Get("0").Int())
			area.Name = street.Get("1").String()
			area.TraName = jsonT.Get(fmt.Sprintf("%d.1", i)).String()
			area.ParentID = int(street.Get("2").Int())
			area.classID = 3
			log.Printf("%v", area)
			areas = append(areas, area)
		}
		err := gocsv.MarshalCSVWithoutHeaders(&areas, csvWriter)
		if err != nil {
			log.Printf("error: %s", err)
		}
		csvWriter.Flush()
	}
	wg.Done()
}
