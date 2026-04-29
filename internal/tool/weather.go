package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	tutils "github.com/cloudwego/eino/components/tool/utils"
)

// weatherInput 与 Eino 官方示例 / 测试中常见的 get_weather 参数一致：location、unit（c|f）。
type weatherInput struct {
	Location string `json:"location" jsonschema:"description=要查询天气的地点，城市或地区名，如 北京、上海、London"`
	Unit     string `json:"unit" jsonschema:"description=温度单位：c 为摄氏（默认），f 为华氏"`
}

// NewGetWeatherTool 返回 get_weather：相对 calculator 演示了「外部 HTTP + 副作用」工具；入参 location/unit 与 Eino 常见示例一致。1
// 实现里用 ctx 控制请求超时，便于与 Runner 取消策略配合。数据来自 Open-Meteo，无需单独申请密钥。
func NewGetWeatherTool() (tool.InvokableTool, error) {
	return tutils.InferTool("get_weather", "根据地点查询当前天气（气温与概况）。当用户问某地天气、温度、是否下雨等均应调用。参数 location 为地点；unit 可选 c 或 f。", func(ctx context.Context, in weatherInput) (string, error) {
		loc := strings.TrimSpace(in.Location)
		if loc == "" {
			return "", fmt.Errorf("location 不能为空")
		}
		unit := strings.ToLower(strings.TrimSpace(in.Unit))
		if unit == "" {
			unit = "c"
		}
		if unit != "c" && unit != "f" {
			return "", fmt.Errorf("unit 只支持 c 或 f，收到: %s", in.Unit)
		}

		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return fetchOpenMeteoWeather(cctx, loc, unit)
	})
}

func fetchOpenMeteoWeather(ctx context.Context, place, unit string) (string, error) {
	hc := &http.Client{Timeout: 20 * time.Second}
	lat, lon, displayName, err := geocode(ctx, hc, place)
	if err != nil {
		return "", err
	}

	tempParam := "celsius"
	if unit == "f" {
		tempParam = "fahrenheit"
	}
	u, err := url.Parse("https://api.open-meteo.com/v1/forecast")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("latitude", fmt.Sprintf("%g", lat))
	q.Set("longitude", fmt.Sprintf("%g", lon))
	q.Set("current", "temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m")
	q.Set("timezone", "auto")
	q.Set("temperature_unit", tempParam)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("天气接口请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("天气接口状态码: %d", resp.StatusCode)
	}

	var body struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			Humidity    int     `json:"relative_humidity_2m"`
			WeatherCode int     `json:"weather_code"`
			WindSpeed   float64 `json:"wind_speed_10m"`
			Time        string  `json:"time"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("解析天气数据: %w", err)
	}

	unitLabel := "°C"
	if unit == "f" {
		unitLabel = "°F"
	}
	desc := wmoCodeDesc(body.Current.WeatherCode)
	return fmt.Sprintf(
		"地点: %s（%s, %s）\n观测时间(当地): %s\n气温: %.1f%s，湿度: %d%%，风速: %.0f km/h，天气: %s\n数据来源: Open-Meteo",
		displayName, fmtAngle(lat), fmtAngle(lon), body.Current.Time, body.Current.Temperature, unitLabel, body.Current.Humidity, body.Current.WindSpeed, desc,
	), nil
}

func fmtAngle(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

func geocode(ctx context.Context, hc *http.Client, place string) (lat, lon float64, name string, err error) {
	u, err := url.Parse("https://geocoding-api.open-meteo.com/v1/search")
	if err != nil {
		return 0, 0, "", err
	}
	q := u.Query()
	q.Set("name", place)
	q.Set("count", "1")
	q.Set("language", "zh")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, 0, "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("地理编码请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, "", fmt.Errorf("地理编码状态码: %d", resp.StatusCode)
	}

	var g struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return 0, 0, "", fmt.Errorf("解析地理编码: %w", err)
	}
	if len(g.Results) == 0 {
		return 0, 0, "", fmt.Errorf("未找到地点: %q，请换用更具体的城市名", place)
	}
	r := g.Results[0]
	n := r.Name
	if r.Admin1 != "" {
		n = r.Name + ", " + r.Admin1
	}
	if r.Country != "" {
		n += ", " + r.Country
	}
	return r.Latitude, r.Longitude, n, nil
}

func wmoCodeDesc(code int) string {
	// 简化 WMO weather code 说明
	switch code {
	case 0:
		return "晴"
	case 1, 2, 3:
		return "多云"
	case 45, 48:
		return "有雾"
	case 51, 53, 55:
		return "小毛毛雨/细雨"
	case 56, 57:
		return "冻毛毛雨"
	case 61, 63, 65:
		return "有雨"
	case 66, 67:
		return "冻雨"
	case 71, 73, 75:
		return "有雪"
	case 77:
		return "雪粒"
	case 80, 81, 82:
		return "阵雨"
	case 85, 86:
		return "阵雪"
	case 95:
		return "雷暴"
	case 96, 99:
		return "雷暴伴冰雹"
	default:
		return fmt.Sprintf("代码 %d", code)
	}
}
