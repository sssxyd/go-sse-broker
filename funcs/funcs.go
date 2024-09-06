package funcs

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

func GetExecutionPath() string {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Error getting executable path:", err)
		return ""
	}
	exeDir := filepath.Dir(exePath)

	// 判断是否在临时目录中运行（典型的 go run 行为）
	if strings.Contains(exePath, os.TempDir()) {
		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			fmt.Println("Failed to get caller information")
			return ""
		}
		srcDir := filepath.Dir(filename)
		return filepath.Dir(srcDir)
	} else {
		// 默认返回可执行文件所在目录
		return exeDir
	}
}

func InitializeLogFile(logFilePath string, stdOut bool) *os.File {
	// 设置日志前缀包含长文件名和行号
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime | log.Lmicroseconds)

	// 指定日志文件的路径
	logFilDir := filepath.Dir(logFilePath)
	// 确保目录存在
	if err := os.MkdirAll(logFilDir, 0755); err != nil {
		log.Fatalf("Failed to create directory: %v", err)
	}
	// 打开或创建日志文件
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}

	if stdOut {
		//设置 MultiWriter，同时输出到文件和 stdout
		mw := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(mw)
	} else {
		log.SetOutput(logFile)
	}
	return logFile
}

func IsPathExist(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	} else if err != nil {
		log.Printf("An error occurred while checking the path %s: %v\n", path, err)
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
