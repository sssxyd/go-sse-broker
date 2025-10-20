package funcs

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	callerProjectInfoCache *project_info
	callerProjectInfoOnce  sync.Once
	app_data_path          string
)

// ProjectInfo 项目信息
type project_info struct {
	RootPath    string
	ModuleName  string
	ProjectName string
}

func (p *project_info) is_internal() bool {
	return p.ModuleName == "std" || p.ModuleName == "builtin"
}

func SetAppDataPath(path string) {
	if path == "" {
		fmt.Println("SetAppDataPath: path cannot be empty")
		return
	}
	app_data_path = path
}

func _find_project_root_from_path(startPath string) string {
	currentDir := startPath

	for {
		goModPath := filepath.Join(currentDir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return currentDir
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			break
		}
		currentDir = parentDir
	}

	return ""
}

func _parse_go_mod(rootPath string) (moduleName, projectName string) {
	goModPath := filepath.Join(rootPath, "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return "", ""
	}

	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			moduleName = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			if parts := strings.Split(moduleName, "/"); len(parts) > 0 {
				projectName = parts[len(parts)-1]
			}
			break
		}
	}

	return moduleName, projectName
}

func _get_caller_project_info_safe() *project_info {
	callerProjectInfoOnce.Do(func() {
		callerProjectInfoCache = _get_caller_project_info_internal()
	})
	return callerProjectInfoCache
}

func _get_caller_project_info_internal() *project_info {
	var lastValidProject *project_info
	var readedGoMods = make(map[string]*project_info)

	for skip := 1; skip < 100; skip++ {
		_, filename, _, ok := runtime.Caller(skip)
		if !ok {
			break
		}

		filename = filepath.Clean(filename)
		if filename == "" {
			continue
		}

		// 尝试找到项目根目录
		fileDir := filepath.Dir(filename)
		rootPath := _find_project_root_from_path(fileDir)

		if rootPath == "" {
			continue
		}

		currentProject, exists := readedGoMods[rootPath]
		if !exists {
			moduleName, projectName := _parse_go_mod(rootPath)
			if moduleName == "" {
				// 如果没有找到 go.mod 文件，可能是非模块化项目
				continue
			}
			currentProject = &project_info{
				RootPath:    rootPath,
				ModuleName:  moduleName,
				ProjectName: projectName,
			}
		}
		readedGoMods[rootPath] = currentProject
		if !currentProject.is_internal() {
			// 如果是非标准库或内置库，则认为是有效的项目
			lastValidProject = currentProject
		}
	}

	if lastValidProject != nil {
		return lastValidProject
	}

	return &project_info{RootPath: "", ModuleName: "", ProjectName: "unknown"}
}

// 临时目录判断
func _is_running_from_temp(exePath string) bool {
	tempDirs := []string{
		os.TempDir(),
		"/tmp",     // Unix/Linux
		"/var/tmp", // Unix/Linux alternative
	}

	for _, tempDir := range tempDirs {
		if strings.Contains(exePath, tempDir) {
			return true
		}
	}
	return false
}

func _is_running_as_debug(exeName string) bool {
	// 检查是否在调试模式下运行
	// 这里假设调试模式下的可执行文件名包含 "debug" 或者 "test"
	debugIndicators := []string{"debug", "test"}
	for _, indicator := range debugIndicators {
		if strings.Contains(exeName, indicator) {
			return true
		}
	}
	return false
}

func GetAppRootPath() string {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Error getting executable path:", err)
		return ""
	}
	exeDir := filepath.Dir(exePath)

	// 判断是否在临时目录中运行（典型的 go run 行为）
	if _is_running_from_temp(exePath) || _is_running_as_debug(filepath.Base(exePath)) {
		info := _get_caller_project_info_safe()
		if info.RootPath != "" {
			// 如果获取到调用者的项目根目录，则返回该目录
			return info.RootPath
		} else {
			// 如果没有获取到调用者的项目根目录，则返回当前执行文件所在目录
			return exeDir
		}
	} else {
		// 默认返回可执行文件所在目录
		return exeDir
	}
}

func InitializeLumberjackLogger(logFilePath string, maxMegaBytes int, maxAgeDays int, maxBackups int, compress bool) *lumberjack.Logger {
	// 检查日志目录是否存在
	logDir := filepath.Dir(logFilePath)
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		// 如果目录不存在，则创建它
		err := os.MkdirAll(logDir, os.ModePerm) // 递归创建目录
		if err != nil {
			slog.Error("Failed to create log directory", "error", err)
			return nil
		}
	}

	if maxMegaBytes <= 0 {
		maxMegaBytes = 100
	}
	if maxAgeDays < 0 {
		maxAgeDays = 0
	}

	// 配置 lumberjack 日志文件管理
	logger := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    maxMegaBytes, // 每个日志文件最大大小（单位：MB）
		MaxBackups: maxBackups,   // 保留的旧日志文件个数
		MaxAge:     maxAgeDays,   // 日志文件保留天数
		Compress:   compress,     // 自动压缩旧日志文件
		LocalTime:  true,         // 使用本地时间
	}

	return logger
}

func IsPathExist(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	} else if err != nil {
		slog.Error("Failed to check path", "path", path, "error", err)
		return false
	} else {
		return true
	}
}

func SqlInValues(size int) string {
	placeholders := make([]string, size)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return "(" + strings.Join(placeholders, ",") + ")"
}

func SqlToParams(inputs ...interface{}) []interface{} {
	var result []interface{}
	for _, input := range inputs {
		// 利用反射判断输入是否为切片
		reflectedInput := reflect.ValueOf(input)
		if reflectedInput.Kind() == reflect.Slice {
			// 遍历切片，将元素逐一添加到结果切片
			for i := 0; i < reflectedInput.Len(); i++ {
				result = append(result, reflectedInput.Index(i).Interface())
			}
		} else {
			// 非切片类型直接添加到结果切片
			result = append(result, input)
		}
	}
	return result
}

func TouchDir(path string) error {
	// 检查目录是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// 创建目录
		err := os.MkdirAll(path, 0755) // 0755 权限设置允许所有者读写执行，组和其他用户只读执行
		if err != nil {
			return err
		}
	}
	return nil
}

func MD5(text string) string {
	hash := md5.Sum([]byte(text))
	md5String := hex.EncodeToString(hash[:])
	return md5String
}
