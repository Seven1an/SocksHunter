package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-ini/ini"
)

// 定义响应结构体
type FofaResponse struct {
	Error           bool       `json:"error"`
	ConsumedFpoint  int        `json:"consumed_fpoint"`
	RequiredFpoints int        `json:"required_fpoints"`
	Size            int        `json:"size"`
	Tip             string     `json:"tip"`
	Page            int        `json:"page"`
	Mode            string     `json:"mode"`
	Query           string     `json:"query"`
	Results         [][]string `json:"results"`
	Errmsg          string     `json:"errmsg"` // 增加 errmsg 字段
}

// 检查代理是否可用
func checkProxy(socks5Addr string, wg *sync.WaitGroup, availableProxies *[]string, mutex *sync.Mutex) {
	defer wg.Done()

	testURL := "https://www.baidu.com"

	proxyURL, err := url.Parse("socks5://" + socks5Addr)
	if err != nil {
		return
	}

	tr := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{
				Timeout: 5 * time.Second,
			}
			return d.DialContext(ctx, network, addr)
		},
	}

	client := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}

	response, err := client.Get(testURL)
	if err != nil {
		return
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusOK {
		mutex.Lock()
		*availableProxies = append(*availableProxies, socks5Addr)
		mutex.Unlock()

		// 打印可用代理到控制台
		fmt.Printf("[+] Available: %s\n", socks5Addr)
	}
}

// 更新 v2ray 配置文件
func updateV2rayConfig(proxyAddr string, totalAvailable int) {
	// 解析代理地址
	parts := strings.Split(proxyAddr, ":")
	if len(parts) != 2 {
		log.Fatalf("Error parsing proxy address: %v\n", proxyAddr)
		return
	}
	proxyIP := parts[0]
	proxyPort, err := strconv.Atoi(parts[1])
	if err != nil {
		log.Fatalf("Error parsing proxy port: %v\n", err)
	}

	config := map[string]interface{}{
		"inbounds": []map[string]interface{}{
			{
				"port":     8888,
				"listen":   "127.0.0.1",
				"protocol": "socks",
				"settings": map[string]interface{}{
					"auth": "noauth",
					"udp":  true,
				},
			},
		},
		"outbounds": []map[string]interface{}{
			{
				"protocol": "socks",
				"settings": map[string]interface{}{
					"servers": []map[string]interface{}{
						{
							"address": proxyIP,
							"port":    proxyPort,
						},
					},
				},
			},
		},
	}

	file, err := os.Create("./v2ray-windows-64/config.json")
	if err != nil {
		log.Fatalf("Error creating config file: %v\n", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		log.Fatalf("Error writing config file: %v\n", err)
	}
}

// 停止 v2ray 实例的函数
func stopV2ray() {
	// 停止 v2ray 进程
	cmd := exec.Command("taskkill", "/F", "/IM", "v2ray.exe")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error stopping v2ray: %v\n", err)
	}
}

// 启动 v2ray 实例的函数
func startV2ray() {
	// 启动 v2ray
	nulFile, err := os.Open("NUL")
	if err != nil {
		log.Fatalf("无法打开 NUL: %v", err)
	}
	defer nulFile.Close()

	cmd := exec.Command("./v2ray-windows-64/v2ray.exe", "run", "-config", "./v2ray-windows-64/config.json")
	cmd.Stdout = nulFile
	cmd.Stderr = nulFile
	if err := cmd.Start(); err != nil {
		log.Fatalf("Error starting v2ray: %v\n", err)
	}

}

// 启动本地代理服务器，按顺序使用可用代理列表
func main() {
	info :=
		`====================================================
 ██╗   ██╗███████╗██╗  ██╗███████╗███████╗ ██████╗
 ╚██╗ ██╔╝██╔════╝╚██╗██╔╝██╔════╝██╔════╝██╔════╝
  ╚████╔╝ █████╗   ╚███╔╝ ███████╗█████╗  ██║     
   ╚██╔╝  ██╔══╝   ██╔██╗ ╚════██║██╔══╝  ██║     
    ██║   ███████╗██╔╝ ██╗███████║███████╗╚██████╗
    ╚═╝   ╚══════╝╚═╝  ╚═╝╚══════╝╚══════╝ ╚═════╝   
 [SocksHunter]		      By:Seven1an    v0.1
====================================================`
	fmt.Println(info)

	// 从 config.ini 文件中读取 apiKey
	cfg, err := ini.Load("config.ini")
	if err != nil {
		fmt.Printf("Error loading config file: %v\n", err)
		return
	}

	apiKey := cfg.Section("fofa").Key("apiKey").String()
	if apiKey == "" {
		fmt.Println("API key is missing in config.ini")
		return
	}

	// 构建请求 URL
	baseURL := "https://fofa.info/api/v1/search/all"
	qbase64 := "cHJvdG9jb2w9InNvY2tzNSIgJiYgIlZlcnNpb246NSBNZXRob2Q6Tm8gQXV0aGVudGljYXRpb24oMHgwMCkiICYmIGNvdW50cnk9IkNOIg=="
	size := "50"

	// 使用 net/url 包安全地构建 URL
	requestURL := fmt.Sprintf("%s?key=%s&qbase64=%s&size=%s", baseURL, url.QueryEscape(apiKey), url.QueryEscape(qbase64), size)

	// 发起 HTTP 请求
	response, err := http.Get(requestURL)
	if err != nil {
		fmt.Printf("Connection to Fofa error: %v\n", err)
		return
	}
	defer response.Body.Close()

	// 检查响应状态码
	if response.StatusCode != http.StatusOK {
		fmt.Printf("Request failed with status: %s\n", response.Status)
		return
	}

	// 读取响应体
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Printf("Error reading response body: %v\n", err)
		return
	}

	// 解析 JSON 响应
	var fofaResp FofaResponse
	if err := json.Unmarshal(body, &fofaResp); err != nil {
		fmt.Printf("Error parsing JSON response: %v\n", err)
		return
	}

	// 检查 API 密钥是否有效
	if fofaResp.Errmsg == "[-700] 账号无效" {
		fmt.Println("Error APIKey!\tExit")
		return
	}

	// 输出总结果数量
	now := time.Now()
	formattedTime := now.Format("2006-01-02 15:04:05")
	fmt.Printf("CurrentDate:%s AddressTotal: [%d]\n", formattedTime, fofaResp.Size)

	// 询问用户想要检测多少条
	var userCount int
	for {
		fmt.Printf("How many proxies do you want to check? (Max: %d): ", fofaResp.Size)
		fmt.Scanln(&userCount)

		// 检查用户输入的条目数是否合法
		if userCount > 0 && userCount <= fofaResp.Size {
			break
		} else {
			fmt.Println("Invalid input, please enter a number less than or equal to the total size.")
		}
	}

	// 并发检查每个 IP 是否可用
	var wg sync.WaitGroup
	var availableProxies []string
	var mutex sync.Mutex

	// 根据用户输入的数量进行检测
	for i, result := range fofaResp.Results {
		if i >= userCount { // 控制检测条目数
			break
		}

		if len(result) >= 2 {
			ipPort := fmt.Sprintf("%s", result[0])
			wg.Add(1)
			go checkProxy(ipPort, &wg, &availableProxies, &mutex)
		}
	}

	wg.Wait()

	if len(availableProxies) > 0 {
		// 生成时间戳
		timestamp := time.Now().Format("20060102_150405")
		filename := fmt.Sprintf("./AvailableList/available_proxies_%s.txt", timestamp)

		// 打开文件并写入可用 IP 和端口
		file, err := os.Create(filename)
		if err != nil {
			fmt.Printf("Error creating file: %v\n", err)
			return
		}
		defer file.Close()

		for _, proxy := range availableProxies {
			file.WriteString(proxy + "\n")
		}

		fmt.Printf("Available list saved ==> %s\n", filename)
		fmt.Println("Socks port: [8888]\n")

		// 可用代理
		for i, proxy := range availableProxies {
			fmt.Printf("Using proxy [%d/%d]: %s\n", i+1, len(availableProxies), proxy)
			updateV2rayConfig(proxy, len(availableProxies))

			// 刷新标准输出
			os.Stdout.Sync()

			// 如果是最后一个代理，或者只有一个代理
			if len(availableProxies) == 1 || i == len(availableProxies)-1 {
				// 只提示退出
				fmt.Println("This is the last proxy. Enter 'q' to quit: ")

				// 等待用户输入 'q' 退出
				for {
					var input string
					fmt.Scanln(&input)

					input = strings.ToLower(input)
					if input == "q" {
						// 用户选择了 'q'，退出程序
						fmt.Println("Exiting...")
						return // 退出整个程序
					} else {
						// 无效输入，提示用户再次输入
						fmt.Println("Invalid input, please enter 'q' to quit.")
					}
				}
			} else {
				// 提示用户继续或退出
				for {
					var input string
					fmt.Println("Enter 'n' to use the next proxy or 'q' to quit: ")
					fmt.Scanln(&input)

					input = strings.ToLower(input)
					if input == "n" {
						// 用户选择了 'n'，继续使用下一个代理
						fmt.Println("Switching to the next proxy...")
						break // 跳出当前循环，继续到下一个代理
					} else if input == "q" {
						// 用户选择了 'q'，退出程序
						fmt.Println("Exiting...")
						return // 退出整个程序
					} else {
						// 无效输入，提示用户再次输入
						fmt.Println("Invalid input, please enter 'n' for next proxy or 'q' to quit.")
					}
				}
			}

			// 停止上一个 v2ray 实例
			stopV2ray()

			// 启动新的 v2ray 实例
			startV2ray()
		}

	} else {
		fmt.Println("No available proxies found.")
	}
}
