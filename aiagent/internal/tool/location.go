package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	tutils "github.com/cloudwego/eino/components/tool/utils"
)

// geolocationInput 高德地理位置：经纬度逆地理编码，或 IP 定位（国内 IPv4）。
type geolocationInput struct {
	Location string `json:"location" jsonschema:"description=可选。GPS/地图坐标，格式为「经度,纬度」（高德 GCJ-02，如 116.397428,39.90923）。填写后与 ip 互斥优先用本字段做逆地理编码。"`
	IP       string `json:"ip" jsonschema:"description=可选。国内 IPv4。不填时程序会先经公网服务探测本机出口 IPv4 再交给高德（可设 CS_DISABLE_PUBLIC_IP_PROBE=1 关闭探测）。"`
}

const (
	amapIPAPI    = "https://restapi.amap.com/v3/ip"
	amapRegeoAPI = "https://restapi.amap.com/v3/geocode/regeo"
)

// NewGeolocationTool 返回 get_geolocation：调用高德开放平台 Web 服务（需 AMAP_KEY），提供 IP 定位或经纬度逆地理编码，不涉及机房/主机名等运维信息。
func NewGeolocationTool() (tool.InvokableTool, error) {
	desc := `地理位置查询（高德地图 Web 服务）。用户问「我在哪」「当前位置在哪」「GPS 地址」「根据经纬度这是哪」等时使用。
- 若有经纬度：在 location 填「经度,纬度」（逗号分隔），走逆地理编码得到文字地址。
- 否则走 IP 定位：可填用户公网 IPv4；不填时会先探测本机出口公网 IPv4 再调用高德（比依赖高德从 HTTP 连接猜 IP 更可靠）。
应答用户时必须引用工具返回的省、市、adcode、城市矩形范围/近似中心（若有），不得只回答「中国」等国家名而忽略工具明细。
勿编造坐标或行政区；未配置 AMAP_KEY 时应提示用户配置密钥。`
	return tutils.InferTool("get_geolocation", desc, func(ctx context.Context, in geolocationInput) (string, error) {
		key := strings.TrimSpace(os.Getenv("AMAP_KEY"))
		if key == "" {
			key = strings.TrimSpace(os.Getenv("GAODE_WEB_KEY"))
		}
		if key == "" {
			return "", fmt.Errorf("未配置高德 Web 服务 Key：请设置环境变量 AMAP_KEY（或 GAODE_WEB_KEY）")
		}
		loc := strings.TrimSpace(in.Location)
		ip := strings.TrimSpace(in.IP)
		if loc != "" {
			return amapRegeo(ctx, key, loc)
		}
		return amapIPLocate(ctx, key, ip)
	})
}

func amapIPLocate(ctx context.Context, key, ip string) (string, error) {
	var usedProbe string
	if strings.TrimSpace(ip) == "" && strings.TrimSpace(os.Getenv("CS_DISABLE_PUBLIC_IP_PROBE")) == "" {
		if pub, err := discoverPublicIPv4(ctx); err == nil {
			ip = pub
			usedProbe = pub
		}
	}
	u, err := url.Parse(amapIPAPI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("key", key)
	if strings.TrimSpace(ip) != "" {
		q.Set("ip", strings.TrimSpace(ip))
	}
	q.Set("output", "JSON")
	u.RawQuery = q.Encode()

	body, err := amapGET(ctx, u.String())
	if err != nil {
		return "", err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", fmt.Errorf("解析高德 IP 定位响应: %w", err)
	}
	status := m["status"]
	info := amapIfaceToString(m["info"])
	if !amapStatusOK(status) {
		return "", fmt.Errorf("高德 IP 定位失败: %s", info)
	}
	report := buildAmapIPLocateReport(m)
	if usedProbe != "" {
		report = fmt.Sprintf("【本次高德 v3/ip 使用的 IPv4】%s（由本机经公网探测服务得到，非用户手填；非手机 GPS）。\n\n%s", usedProbe, report)
	}
	return report, nil
}

// discoverPublicIPv4 通过公网 HTTP 服务探测出口 IPv4，供高德 ip 参数显式传入（服务端直连高德时常无法依赖「由连接猜 IP」）。
var publicIPv4ProbeURLs = []string{
	"https://api.ipify.org",
	"https://icanhazip.com",
}

func discoverPublicIPv4(ctx context.Context) (string, error) {
	var lastErr error
	for _, raw := range publicIPv4ProbeURLs {
		s, err := httpGetString(ctx, raw, 8*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		s = strings.TrimSpace(s)
		if isRoutablePublicIPv4(s) {
			return s, nil
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("探测公网 IPv4 失败: %w", lastErr)
	}
	return "", fmt.Errorf("探测公网 IPv4 失败: 未得到可用地址")
}

func httpGetString(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "aiagent-geolocation/1.0")
	hc := &http.Client{Timeout: timeout}
	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 512))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func isRoutablePublicIPv4(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if ip4.IsLoopback() || ip4.IsPrivate() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() {
		return false
	}
	return true
}

// buildAmapIPLocateReport 将 v3/ip 返回格式化为模型易引用的明细（含矩形近似中心），避免被概括成仅国家名。
func buildAmapIPLocateReport(m map[string]interface{}) string {
	province := amapIfaceToString(m["province"])
	city := amapIfaceToString(m["city"])
	adcode := amapIfaceToString(m["adcode"])
	rectangle := amapIfaceToString(m["rectangle"])
	infocode := amapIfaceToString(m["infocode"])

	var b strings.Builder
	b.WriteString("【高德 IP 定位】Web 服务 v3/ip 返回（依据出口 IP；非手机 GPS；省/市/adcode 以高德为准）。\n")
	if province != "" {
		fmt.Fprintf(&b, "省/直辖市: %s\n", province)
	}
	if city != "" {
		fmt.Fprintf(&b, "城市: %s\n", city)
	}
	if adcode != "" {
		fmt.Fprintf(&b, "行政区划代码(adcode): %s\n", adcode)
	}
	if rectangle != "" {
		fmt.Fprintf(&b, "城市范围矩形(原始): %s\n", rectangle)
		if human := formatAmapRectangleHuman(rectangle); human != "" {
			fmt.Fprintf(&b, "城市范围(可读): %s\n", human)
		}
	}
	if infocode != "" {
		fmt.Fprintf(&b, "infocode: %s\n", infocode)
	}

	skip := map[string]struct{}{
		"status": {}, "info": {}, "infocode": {}, "province": {}, "city": {}, "adcode": {}, "rectangle": {},
	}
	var extraKeys []string
	for k := range m {
		if _, ok := skip[k]; ok {
			continue
		}
		if amapIfaceToString(m[k]) == "" {
			continue
		}
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		fmt.Fprintf(&b, "%s: %s\n", k, amapIfaceToString(m[k]))
	}

	b.WriteString("\n【答复用户】请按上一段明细转述：至少包含省/市（若有）、adcode（若有）、矩形或近似中心（若有）。\n")
	b.WriteString("勿仅用常识概括为「中国」——除非工具返回里确实没有省/市/adcode/矩形等任何可用字段。\n")
	summary := buildIPLocalitySummary(province, city, adcode, rectangle)
	fmt.Fprintf(&b, "一句话汇总: %s\n", summary)
	return strings.TrimSpace(b.String())
}

func buildIPLocalitySummary(province, city, adcode, rectangle string) string {
	if province == "" && city == "" && adcode == "" && strings.TrimSpace(rectangle) == "" {
		return "高德未返回省/市/adcode/矩形（常见：境外或不可用 IPv4、局域网；官方说明 IP 定位仅支持国内 IPv4）。请如实说明，勿编造省市区。"
	}
	var parts []string
	if province != "" {
		parts = append(parts, province)
	}
	if city != "" {
		parts = append(parts, city)
	}
	if adcode != "" {
		parts = append(parts, "adcode "+adcode)
	}
	if rectangle != "" {
		if h := formatAmapRectangleHuman(rectangle); h != "" {
			parts = append(parts, h)
		} else {
			parts = append(parts, "范围 "+rectangle)
		}
	}
	return strings.Join(parts, "，")
}

// formatAmapRectangleHuman 解析 rectangle「左下经度,左下纬度;右上经度,右上纬度」，给出近似中心（GCJ-02）。
func formatAmapRectangleHuman(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	segs := strings.Split(s, ";")
	if len(segs) != 2 {
		return ""
	}
	lng1, lat1, ok1 := parseLonLatPair(segs[0])
	lng2, lat2, ok2 := parseLonLatPair(segs[1])
	if !ok1 || !ok2 {
		return ""
	}
	clng := (lng1 + lng2) / 2
	clat := (lat1 + lat2) / 2
	return fmt.Sprintf("左下(%g,%g) 右上(%g,%g)；近似中心(%.5f,%.5f)", lng1, lat1, lng2, lat2, clng, clat)
}

func parseLonLatPair(s string) (lng, lat float64, ok bool) {
	p := strings.Split(strings.TrimSpace(s), ",")
	if len(p) != 2 {
		return 0, 0, false
	}
	lng, err1 := strconv.ParseFloat(strings.TrimSpace(p[0]), 64)
	lat, err2 := strconv.ParseFloat(strings.TrimSpace(p[1]), 64)
	return lng, lat, err1 == nil && err2 == nil
}

// amapIfaceToString 将 JSON 解码后的任意字段转为展示用字符串，兼容 string、number、数组等。
func amapIfaceToString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%.0f", x)
		}
		return strings.TrimSpace(fmt.Sprint(x))
	case bool:
		if x {
			return "true"
		}
		return "false"
	case []interface{}:
		var parts []string
		for _, e := range x {
			if s := amapIfaceToString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "")
	case map[string]interface{}:
		raw, err := json.Marshal(x)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(x))
		}
		return strings.TrimSpace(string(raw))
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func amapRegeo(ctx context.Context, key, location string) (string, error) {
	parts := strings.Split(location, ",")
	if len(parts) != 2 {
		return "", fmt.Errorf("location 格式应为「经度,纬度」，收到: %q", location)
	}
	lng := strings.TrimSpace(parts[0])
	lat := strings.TrimSpace(parts[1])
	if lng == "" || lat == "" {
		return "", fmt.Errorf("经度或纬度为空: %q", location)
	}
	u, err := url.Parse(amapRegeoAPI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("key", key)
	q.Set("location", lng+","+lat)
	q.Set("radius", "1000")
	q.Set("extensions", "base")
	q.Set("output", "JSON")
	u.RawQuery = q.Encode()

	body, err := amapGET(ctx, u.String())
	if err != nil {
		return "", err
	}
	var resp struct {
		Status    any    `json:"status"`
		Info      string `json:"info"`
		Regeocode *struct {
			FormattedAddress string `json:"formatted_address"`
		} `json:"regeocode"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析高德逆地理编码响应: %w", err)
	}
	if !amapStatusOK(resp.Status) {
		return "", fmt.Errorf("高德逆地理编码失败: %s", resp.Info)
	}
	addr := ""
	if resp.Regeocode != nil {
		addr = strings.TrimSpace(resp.Regeocode.FormattedAddress)
	}
	if addr == "" {
		return fmt.Sprintf("【高德逆地理编码】坐标 %s,%s 未返回 formatted_address", lng, lat), nil
	}
	return fmt.Sprintf("【高德逆地理编码】坐标 %s,%s（GCJ-02）\n大致地址: %s", lng, lat, addr), nil
}

func amapGET(ctx context.Context, rawURL string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	hc := &http.Client{Timeout: 15 * time.Second}
	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求高德 API: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("高德 API HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func amapStatusOK(v any) bool {
	switch x := v.(type) {
	case string:
		return x == "1"
	case float64:
		return x == 1
	case json.Number:
		return x.String() == "1"
	default:
		return false
	}
}
