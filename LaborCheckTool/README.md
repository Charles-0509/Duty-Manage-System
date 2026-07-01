# 劳务记录表二次检查工具

这个工具用于在提交教务前，检查“劳务转换”导出的 Excel 和“下载记录表”得到的 zip 是否一致。

它是独立 Go module，不读取 DMS 数据库，不读取或写入主程序 `data/` 目录。运行时只会读取你通过命令行传入的 Excel 和 zip；只有在指定 `-out` 时才会额外写出一份文本报告。

## 检查内容

- Excel 中每个人的调整后工时/金额是否能和记录表明细工时对应。
- 记录表 zip 是否缺少 Excel 中的人。
- 记录表 zip 是否多出 Excel 中没有的人。
- 每个 docx 记录表的明细行合计是否等于“合计小时数”单元格。
- 总工时与折算金额是否一致，折算标准为 `25 元/小时`。

> 当前 DMS 导出的劳务计算 Excel 第 G 列是记录表使用的调整后工时口径；如果后续改成金额口径，本工具会在 G 列数值大于 200 时自动按金额除以 25 还原工时。

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
