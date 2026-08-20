// Package handler 登录密码哈希辅助函数（T040: 渐进式重哈希）
package handler

import (
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const DefaultBcryptCost = 8 // T040: 新用户/渐进升级的目标成本

// GenerateBcryptHash 以默认成本生成 bcrypt 哈希（T040 优化）
func GenerateBcryptHash(password []byte) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(password, DefaultBcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CompareBcryptHash 比较 bcrypt 哈希（自适应成本，兼容 cost8/9/10）
func CompareBcryptHash(hash, password []byte) error {
	return bcrypt.CompareHashAndPassword(hash, password)
}

// ExtractBcryptCost 从 hash 字符串中提取成本参数（如 $2a$10$... → 10）
func ExtractBcryptCost(hash string) int {
	parts := strings.Split(hash, "$")
	if len(parts) >= 3 && (parts[1] == "2a" || parts[1] == "2b" || parts[1] == "2y") {
		cost, _ := strconv.Atoi(parts[2])
		return cost
	}
	return 10 // 默认 fallback
}

// ShouldUpgradeToNewCost 判断是否需要在登录后升级到新成本
func ShouldUpgradeToNewCost(hash string) bool {
	return ExtractBcryptCost(hash) > DefaultBcryptCost
}

// GetDefaultBcryptCost 返回默认成本配置（供 handler 调用）
func GetDefaultBcryptCost() int {
	return DefaultBcryptCost
}
