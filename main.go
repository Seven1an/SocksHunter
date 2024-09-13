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
	Errmsg          string     `json:"errmsg"`
}

// 获取size
func getFofaTotalCount(apiKey string) (int, error) {
	baseURL := "https://fofa.info/api/v1/search/all"
	qbase64 := "cHJvdG9jb2w9InNvY2tzNSIgJiYgIlZlcnNpb246NSBNZXRob2Q6Tm8gQXV0aGVudGljYXRpb24oMHgwMCkiICYmIGNvdW50cnk9IkNOIg=="
	size := "1"

	requestURL := fmt.Sprintf("%s?key=%s&qbase64=%s&size=%s", baseURL, url.QueryEscape(apiKey), url.QueryEscape(qbase64), size)

	response, err := http.Get(requestURL)
	if err != nil {
		return 0, fmt.Errorf("connection to Fofa error: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("request failed with status: %s", response.Status)
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return 0, fmt.Errorf("error reading response body: %v", err)
	}

	var fofaResp FofaResponse
	if err := json.Unmarshal(body, &fofaResp); err != nil {
		return 0, fmt.Errorf("error parsing JSON response: %v", err)
	}

	if fofaResp.Errmsg == "[-700] 账号无效" {
		return 0, fmt.Errorf("error APIKey: %v", fofaResp.Errmsg)
	}

	return fofaResp.Size, nil
}

// 获取代理
func getProxies(apiKey string, size int) ([][]string, error) {
	baseURL := "https://fofa.info/api/v1/search/all"
	qbase64 := "cHJvdG9jb2w9InNvY2tzNSIgJiYgIlZlcnNpb246NSBNZXRob2Q6Tm8gQXV0aGVudGljYXRpb24oMHgwMCkiICYmIGNvdW50cnk9IkNOIg=="

	requestURL := fmt.Sprintf("%s?key=%s&qbase64=%s&size=%d", baseURL, url.QueryEscape(apiKey), url.QueryEscape(qbase64), size)

	response, err := http.Get(requestURL)
	if err != nil {
		return nil, fmt.Errorf("connection to Fofa error: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status: %s", response.Status)
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	var fofaResp FofaResponse
	if err := json.Unmarshal(body, &fofaResp); err != nil {
		return nil, fmt.Errorf("error parsing JSON response: %v", err)
	}

	if fofaResp.Errmsg == "[-700] 账号无效" {
		return nil, fmt.Errorf("error APIKey: %v", fofaResp.Errmsg)
	}

	return fofaResp.Results, nil
}

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
	info := `
====================================================
 ██╗   ██╗███████╗██╗  ██╗███████╗███████╗ ██████╗
 ╚██╗ ██╔╝██╔════╝╚██╗██╔╝██╔════╝██╔════╝██╔════╝
  ╚████╔╝ █████╗   ╚███╔╝ ███████╗█████╗  ██║     
   ╚██╔╝  ██╔══╝   ██╔██╗ ╚════██║██╔══╝  ██║     
    ██║   ███████╗██╔╝ ██╗███████║███████╗╚██████╗
    ╚═╝   ╚══════╝╚═╝  ╚═╝╚══════╝╚══════╝ ╚═════╝   
 [SocksHunter]		      By:Seven1an    v0.1
====================================================`
	fmt.Println(info)

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

	totalCount, err := getFofaTotalCount(apiKey)
	if err != nil {
		fmt.Printf("Error getting total count: %v\n", err)
		return
	}
	now := time.Now()
	formattedTime := now.Format("2006-01-02 15:04:05")
	fmt.Printf("CurrentDate:%s AddressTotal: [%d]\n", formattedTime, totalCount)

	var userCount int
	for {
		fmt.Printf("How many proxies do you want to check? (Max: %d): ", totalCount)
		fmt.Scanln(&userCount)

		if userCount > 0 && userCount <= totalCount {
			break
		} else {
			fmt.Println("Invalid input, please enter a number less than or equal to the total size.")
		}
	}

	proxies, err := getProxies(apiKey, userCount)
	if err != nil {
		fmt.Printf("Error getting proxies: %v\n", err)
		return
	}

	// 并发检查每个 IP 是否可用
	var wg sync.WaitGroup
	var availableProxies []string
	var mutex sync.Mutex

	for _, result := range proxies {
		if len(result) >= 2 {
			ipPort := fmt.Sprintf("%s", result[0])
			wg.Add(1)
			go checkProxy(ipPort, &wg, &availableProxies, &mutex)
		}
	}

	wg.Wait()

	if len(availableProxies) > 0 {
		timestamp := time.Now().Format("20060102_150405")
		filename := fmt.Sprintf("./AvailableList/available_proxies_%s.txt", timestamp)

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

		for i, proxy := range availableProxies {
			fmt.Printf("Using proxy [%d/%d]: %s\n", i+1, len(availableProxies), proxy)
			updateV2rayConfig(proxy, len(availableProxies))

			os.Stdout.Sync()

			if len(availableProxies) == 1 || i == len(availableProxies)-1 {
				fmt.Println("This is the last proxy. Enter 'q' to quit: ")

				for {
					var input string
					fmt.Scanln(&input)

					input = strings.ToLower(input)
					if input == "q" {
						fmt.Println("Exiting...")
						return
					} else {
						fmt.Println("Invalid input, please enter 'q' to quit.")
					}
				}
			} else {
				for {
					var input string
					fmt.Println("Enter 'n' to use the next proxy or 'q' to quit: ")
					fmt.Scanln(&input)

					input = strings.ToLower(input)
					if input == "n" {
						fmt.Println("Switching to the next proxy...")
						break
					} else if input == "q" {
						fmt.Println("Exiting...")
						return
					} else {
						fmt.Println("Invalid input, please enter 'n' for next proxy or 'q' to quit.")
					}
				}
			}

			stopV2ray()
			startV2ray()
		}

	} else {
		fmt.Println("No available proxies found.")
	}
}
