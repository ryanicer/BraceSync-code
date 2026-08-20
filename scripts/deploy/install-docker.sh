#!/usr/bin/env bash
# BraceSync 服务器 Docker 安装脚本
# 用法：ssh 后执行 bash install-docker.sh
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

echo "==> [1/4] 更新 apt ..."
sudo apt-get update -qq

echo "==> [2/4] 安装依赖 ..."
sudo apt-get install -y -qq ca-certificates curl gnupg lsb-release

echo "==> [3/4] 添加 Docker 官方源 ..."
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update -qq

echo "==> [4/4] 安装 Docker Engine + Compose Plugin ..."
sudo apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin

echo "==> 添加 ubuntu 用户到 docker 组 ..."
sudo usermod -aG docker ubuntu

echo ""
echo "=== Docker 安装完成 ==="
docker --version
docker compose version
