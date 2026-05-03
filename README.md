# Line Frame Sampling

沿 `WKT LINESTRING` 轨迹按固定间隔生成采样点，并为每个采样点导出电子边框 `POLYGON`。仓库同时提供 Python 和 Go 两个实现，输入输出格式保持一致。

## Features

- 固定间隔采样，单位为米
- 支持 `square` 和 `circle` 两种边框形状
- 支持从 CSV 中读取 `ez_sys_geom` 字段
- 自动导出 `geom_wkt` 多边形结果
- 支持简单可视化预览
- 对无效行做跳过和提示，不再因坏数据直接崩溃

## Project Layout

```text
.
├── examples/
│   └── sample_input.csv
├── go_version/
│   ├── go.mod
│   ├── go.sum
│   └── main.go
├── py_version/
│   ├── calculate_points.py
│   └── 线路电子边框采样工具.spec
├── app_icon.ico
├── LICENSE
├── README.md
└── requirements.txt
```

## Input Format

输入必须是 CSV，且至少包含这一列：

- `ez_sys_geom`: `LINESTRING(lon lat, lon lat, ...)`

示例：

```csv
name,ez_sys_geom,remark
demo_line,"LINESTRING(121.5000 38.9000, 121.6000 39.0000)",optional
```

仓库自带示例文件：[examples/sample_input.csv](/D:/UGit/Line-frame-sampling/examples/sample_input.csv)

## Python Version

环境准备：

```bash
pip install -r requirements.txt
```

命令行运行：

```bash
python py_version/calculate_points.py examples/sample_input.csv --no-vis
```

查看模板：

```bash
python py_version/calculate_points.py --template
```

常用参数：

- `-o`, `--output`: 输出 CSV 路径
- `-i`, `--interval`: 采样间隔，默认 `100`
- `-s`, `--size`: 方框边长或圆半径，默认 `100`
- `-sh`, `--shape`: `square` 或 `circle`
- `--no-vis`: 禁用可视化
- `-lc`, `--line-color`: 可视化线颜色

## Go Version

首次运行需要 Go 拉取依赖：

```bash
cd go_version
go run . -input ..\examples\sample_input.csv -no-vis
```

也可以直接构建：

```bash
cd go_version
go build .
```

查看模板：

```bash
cd go_version
go run . -t
```

说明：

- Go 版本优先按 UTF-8/UTF-8 BOM 读取 CSV
- 若不是合法 UTF-8，会回退到 `GB18030`
- 无效坐标会跳过并输出告警

## Output Columns

输出 CSV 包含：

- `original_line_index`
- `distance_m`
- `center_lon`
- `center_lat`
- `geom_wkt`

其中 `geom_wkt` 为闭合的 `POLYGON ((...))`。

## Current Improvements

这次补充已覆盖：

- 修复 Python 版本遇到坏 `WKT` 行直接崩溃的问题
- 修复 Go 版本把 UTF-8 中文 CSV 误判为 GBK 的风险
- 修复 Go 版本静默吞掉非法坐标的问题
- 新增 `.gitignore`
- 新增 `requirements.txt`
- 新增示例数据
- 重写根目录 README，使其与当前仓库结构一致

## Suggested Next Steps

如果还要继续完善，建议下一步补这两类：

1. 自动化测试：覆盖 WKT 解析、编码识别、异常输入、导出结果
2. CI 流程：至少做 Python 语法检查和 Go build/test

## 📞 联系与支持

- **QQ**：806666754
- **官方主站**：[laokhome.cn](https://laokhome.cn)
- **Email**：kuai410022283@qq.com

- **捐赠** 如果觉得项目对你有用，可以捐赠任意资金，捐赠的资金，会用来维护项目及开发成本。

![捐赠二维码](捐赠二维码.jpg)


## License

Apache License
