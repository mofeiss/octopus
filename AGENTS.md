# Octopus - Fork 维护指南

## 核心原则：补丁式开发（Patch-Style Development）

本仓库是上游 `bestruirui/octopus` 的长期 fork，自行维护新功能，不向上游提 PR。
为了在同步上游更新时最小化合并冲突，所有改动必须遵循以下原则：

### 1. 补丁优先，避免侵入核心逻辑

- **新增代码优于修改代码**：优先通过新增函数、新增文件、新增字段来实现功能，而不是重写或重构现有核心逻辑。
- **条件追加优于流程重组**：在现有流程末尾追加逻辑（如 `if` 分支、新字段赋值），而不是改变原有控制流。
- **最小改动面**：每个功能的改动应尽量集中在少数几行，避免大面积重排代码或修改函数签名。

### 2. 具体做法

- **后端（Go）**：新增字段时追加到结构体末尾；新增逻辑时在函数末尾追加代码块，用注释 `// [fork]` 标记。
- **前端（React/Next.js）**：新增 UI 元素时在现有元素后追加；新增组件优先放在独立文件中。
- **避免**：重命名现有变量/函数、调整现有代码缩进/格式、移动代码块位置、修改函数参数列表。

### 3. 标记约定

所有 fork 自定义改动使用 `// [fork]` 注释标记，方便后续合并时快速定位。

## 技术栈

- 后端：Go (Gin + GORM)，SQLite/MySQL
- 前端：Next.js 16 + React 19 + Tailwind CSS 4 + Zustand + TanStack Query
- 包管理：Go modules / pnpm
- 构建：`scripts/build.sh` / Dockerfile

## 开发环境

- macOS ARM + zsh
- Node.js: pnpm

## 镜像构建与 GHCR 发布经验

目标镜像：

```text
ghcr.io/mofeiss/octopus:my-dev
ghcr.io/mofeiss/octopus:latest
```

约定：`latest` 代表最后一个已发布版本，SaaS 平台部署固定使用 `latest`，并通过 `pull_policy: always` 强制拉取最新 digest。

### 1. 多架构构建器

在 macOS ARM 本地构建并推送 `linux/amd64`、`linux/arm64` 多架构镜像时，默认 Docker driver 可能不支持 `--platform ... --push`。使用 docker-container driver 创建专用 buildx builder：

```bash
docker buildx create --name codex-multi --driver docker-container --use --bootstrap
docker buildx ls
```

确认 builder 支持：

```text
linux/amd64
linux/arm64
```

### 2. GHCR 登录与权限

推送到 GHCR 需要 GitHub token 具备 `write:packages` 权限。先检查当前登录状态：

```bash
gh auth status -h github.com
```

如果 token scopes 里没有 `write:packages`，推送会失败，常见错误：

```text
denied: permission_denied: The token provided does not match expected scopes.
```

刷新权限：

```bash
gh auth refresh -h github.com --scopes write:packages,read:packages < /dev/null
```

该命令会输出 GitHub device code，需要在浏览器打开 `https://github.com/login/device` 完成授权。

授权完成后登录 GHCR：

```bash
gh auth token | docker login ghcr.io -u mofeiss --password-stdin
```

### 3. 本次有效构建命令

原始 `Dockerfile` 使用 `pnpm@latest` 和默认 npm registry。实际构建时遇到两个问题：

- npm 官方 registry 在 Docker 构建中下载 Next/SWC/Sharp 相关包超时。
- `pnpm@latest` 解析到 pnpm 11 后，会默认忽略 `@swc/core`、`sharp`、`unrs-resolver` 的 build scripts，导致 Next.js 构建不可用。

本次没有修改仓库里的 `Dockerfile`，而是通过 stdin 临时生成构建用 Dockerfile。发布时同时打 `my-dev` 和 `latest` 两个 tag：

```bash
node - <<'NODE' | docker buildx build --builder codex-multi --platform linux/amd64,linux/arm64 --build-arg GIT_VERSION=my-dev -t ghcr.io/mofeiss/octopus:my-dev -t ghcr.io/mofeiss/octopus:latest --push -f - .
const fs = require('fs');
let s = fs.readFileSync('Dockerfile', 'utf8');
s = s.replace('RUN corepack enable && corepack prepare pnpm@latest --activate', 'RUN corepack enable && corepack prepare pnpm@9.15.9 --activate');
s = s.replace(
  'RUN pnpm install --frozen-lockfile',
  `RUN pnpm config set registry https://registry.npmmirror.com && pnpm config set fetch-retries 10 && pnpm config set fetch-retry-mintimeout 20000 && pnpm config set fetch-retry-maxtimeout 180000 && pnpm config set fetch-timeout 900000 && pnpm install --frozen-lockfile`
);
process.stdout.write(s);
NODE
```

关键点：

- `pnpm@9.15.9` 可以正常执行依赖 postinstall scripts。
- `registry.npmmirror.com` 和较长 fetch timeout 能显著降低构建下载失败概率。
- 通过 `-f -` 使用临时 Dockerfile，不污染仓库文件。
- `--build-arg GIT_VERSION=my-dev` 会让启动 banner 里的 Version 显示为 `my-dev`。
- `latest` 必须随每次发布一起更新，保证 SaaS 使用 `latest` 时代表最后一个版本。

如果已经推送了 `my-dev`，但忘记推 `latest`，可以不重建镜像，直接把当前 `my-dev` manifest 复制成 `latest`：

```bash
docker buildx imagetools create -t ghcr.io/mofeiss/octopus:latest ghcr.io/mofeiss/octopus:my-dev
```

### 4. 推送后验证

推送完成后检查远端 manifest：

```bash
docker buildx imagetools inspect ghcr.io/mofeiss/octopus:my-dev
docker buildx imagetools inspect ghcr.io/mofeiss/octopus:latest
```

本次成功推送并同步到 `latest` 的 manifest digest：

```text
sha256:0185c3021e7de0f7a71c4c8fc916d8c4c093e66ef6e66ee79549eb08c75e14ad
```

验证结果包含：

```text
Platform: linux/amd64
Platform: linux/arm64
```

### 5. 排障记录

- 如果提示默认 docker driver 不支持多平台推送，切换到 `docker-container` buildx builder。
- 如果推送 GHCR 被拒绝，优先检查 `gh auth status` 里的 token scopes。
- 如果 pnpm 11 提示 ignored build scripts，不要只改 `onlyBuiltDependencies`；在当前项目构建场景下，固定 `pnpm@9.15.9` 更直接稳定。
- 如果重新执行构建，BuildKit 会复用缓存，通常只需要重新导出和推送镜像层。
- 如果 SaaS 平台重新部署 `latest` 仍使用旧镜像，Compose 里必须加 `pull_policy: always`。

### 6. SaaS 部署写法

SaaS 平台部署使用 `latest`，并强制每次部署拉取远端最新 digest：

```yaml
services:
  octopus:
    image: ghcr.io/mofeiss/octopus:latest
    pull_policy: always
    restart: unless-stopped
    environment:
      PORT: ${PORT-8080}
      NODE_ENV: ${NODE_ENV-production}
      OCTOPUS_DATABASE_TYPE: ${OCTOPUS_DATABASE_TYPE-postgres}
      OCTOPUS_DATABASE_PATH: ${OCTOPUS_DATABASE_PATH-postgresql://postgres:bvbf3o0hcjakdm9k@octopus-postgresql-krmgia:5432/postgres?sslmode=disable}
      OCTOPUS_SERVER_HOST: ${OCTOPUS_SERVER_HOST-0.0.0.0}
      OCTOPUS_SERVER_PORT: ${PORT-8080}
      OCTOPUS_LOG_LEVEL: ${OCTOPUS_LOG_LEVEL-info}
    ports:
      - "8080"
```
