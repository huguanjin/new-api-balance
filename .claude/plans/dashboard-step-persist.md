# Dashboard 分步持久化方案

## 问题
当前 `computeSiteStats` 将所有步骤（分页采集、模型排行、渠道排行、用户排行、错误模型排行）全部在内存中完成。如果最后一步（如错误日志拉取）失败，前面所有步骤的结果都丢失了，必须全部重来。

## 方案
引入 `DashboardComputeTask` 中间任务表，将计算过程拆分为 5 个独立步骤，每步完成后持久化到数据库。再次调用计算时自动跳过已完成步骤，只重新执行失败/未完成的步骤。

### 5 个步骤

| 步骤 | 名称 | 依赖 | 说明 |
|------|------|------|------|
| 1 | `pagination` | 无 | fetchLogStat + 分页采集 type=2 日志，生成中间数据 |
| 2 | `model_ranking` | 步骤1 | 从中间数据取模型候选，调 /api/log/stat 精排 |
| 3 | `channel_ranking` | 步骤1 | 从中间数据取渠道候选，调 /api/log/stat 精排 |
| 4 | `user_ranking` | 步骤1 | 从中间数据取用户候选，调 /api/log/stat 精排 |
| 5 | `error_model_ranking` | 无 | 独立拉取 type=5 错误日志并统计 |

### 文件变更

#### 1. models/models.go — 新增 DashboardComputeTask
#### 2. handlers/db.go — 新增集合和索引
#### 3. handlers/dashboard.go — 拆分 computeSiteStats 为 5 步函数
#### 4. server/main.go — 新增 compute-status 路由
#### 5. Dashboard.vue — 步骤进度显示 + 重试按钮

### 行为变化
- 首次计算：5 步按顺序执行，每步完成后持久化
- 中途失败：已完成步骤数据保留
- 重试：再次调用 compute，自动只跑失败/未完成步骤
- 强制重算：compute 请求加 force: true
