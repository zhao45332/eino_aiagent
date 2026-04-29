package config

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadDotEnv 尝试加载多路径下的 config/.env 与 .env；文件不存在不报错。LoadConfig 内已调用，一般无需再调。
// 顺序：当前工作目录；可执行文件所在目录；可执行文件上一级（例如从 bin/ 启动时仍能找到项目根 config/.env）。
func LoadDotEnv() error {
	var paths []string
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(wd, "config", ".env"), filepath.Join(wd, ".env"))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Clean(filepath.Dir(exe))
		paths = append(paths, filepath.Join(dir, "config", ".env"), filepath.Join(dir, ".env"))
		paths = append(paths, filepath.Join(dir, "..", "config", ".env"), filepath.Join(dir, "..", ".env"))
	}
	for _, p := range paths {
		_ = godotenv.Load(p) //nolint:errcheck
	}
	return nil
}
