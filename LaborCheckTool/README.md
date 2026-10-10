# 劳务记录表二次检查工具

这个工具用于在提交教务前，检查“月度报表”定稿的劳务计算 Excel 和 Word 记录表 ZIP 是否一致。

它是独立 Go module，不读取 DMS 数据库，不读取或写入主程序 `data/` 目录。运行时只会读取你通过命令行传入的 Excel 和 zip；只有在指定 `-out` 时才会额外写出一份文本报告。

## 检查内容

- Excel 中每个人的调整后工时/金额是否能和记录表明细工时对应。
- 记录表 zip 是否缺少 Excel 中的人。
- 记录表 zip 是否多出 Excel 中没有的人。
- 每个 docx 记录表的明细行合计是否等于“合计小时数”单元格。
- 总工时与折算金额是否一致，折算标准为 `25 元/小时`。

当前 DMS 的劳务计算 Excel 第 G 列为申报折算工时，不猜测旧金额格式。零申报成员不要求 Word 记录表。同时核验合法日期、08:00–22:30、每天最多8小时、每条至少2小时、时数与时段一致，以及同日重复或重叠。

## 使用方法

在项目根目录执行：

```powershell
cd .\LaborCheckTool
C:\SDK\Go\bin\go.exe run . -excel "C:\path\调整后劳务计算.xlsx" -records "C:\path\6月勤工助学记录表.zip"
```

生成可保存的报告：

```powershell
C:\SDK\Go\bin\go.exe run . -excel "C:\path\调整后劳务计算.xlsx" -records "C:\path\6月勤工助学记录表.zip" -out ".\report.txt"
```

编译成可执行文件：

```powershell
C:\SDK\Go\bin\go.exe build -o labor-check.exe .
.\labor-check.exe -excel "C:\path\调整后劳务计算.xlsx" -records "C:\path\6月勤工助学记录表.zip"
```

## 退出码

- `0`：检查通过，未发现错误。
- `1`：检查完成，但发现人员、工时或合计不一致。
- `2`：参数错误，或 Excel/zip/docx 无法读取解析。
