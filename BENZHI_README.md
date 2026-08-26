# BENZHI_README

## 项目说明
- 项目：benzhi-project-b43c2787-a1d4-457b-a1a2-f82a75f22e7a
- 项目用途：已完整实现天文底片数字化批次质量验收服务，覆盖建档、标定门禁、扫描版本谱系、确定性质量评估、问题整改、独立抽验、SQLite 封存和连续哈希审计闭环。
- Go 工具链：`golang:1.23.0`
- 前端工具链：无

## 项目描述
- 项目名称：astroplate-vault
- 项目介绍：面向天文底片数字化团队的批次质量验收服务，围绕一个扫描批次从建档、标定、逐张采集、质量判定、异常重扫、同行抽验到科学数据封存的唯一闭环，确保原始底片与数字成果之间具有可验证的谱系和审计证据。
- 项目概述：面向天文底片数字化团队的批次质量验收服务，围绕一个扫描批次从建档、标定、逐张采集、质量判定、异常重扫、同行抽验到科学数据封存的唯一闭环，确保原始底片与数字成果之间具有可验证的谱系和审计证据。
- 核心工作流：数字化操作员创建底片批次并锁定质量规则，提交扫描仪标定证据后逐张登记扫描成果；系统核验完整性并计算质量结论，存在缺陷时要求裁决与重扫替换，质量通过后由独立复核员完成抽样核验，最终由数据保管员生成确定性清单并将批次转为不可修改的封存状态。
- 对外接口：提供版本化 HTTP JSON API 驱动完整批次状态机、查询当前投影与审计事件；服务支持 -addr=127.0.0.1:<port>，也支持通过 PORT 绑定 127.0.0.1:<PORT>，默认监听 127.0.0.1:19081，且不默认绑定 0.0.0.0 或常见低位端口。

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...

cd /app && GOTOOLCHAIN=local go run ./cmd/server -self-check -self-check-timeout=20s -addr=127.0.0.1:19081

cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh

./build_benzhi_docker.sh benzhi-project-b43c2787-a1d4-457b-a1a2-f82a75f22e7a-amd64 linux/amd64

./build_benzhi_docker.sh benzhi-project-b43c2787-a1d4-457b-a1a2-f82a75f22e7a-arm64 linux/arm64

docker run -it benzhi-project-b43c2787-a1d4-457b-a1a2-f82a75f22e7a-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/server -self-check -self-check-timeout=20s -addr=127.0.0.1:19081`
