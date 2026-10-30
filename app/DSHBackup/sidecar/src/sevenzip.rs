use crate::error::{Result, SidecarError};
use crate::pathsafe::{ensure_inside, validate_archive_path};
use crate::protocol::Emitter;
use crate::util::{ensure_dir, hash_file, hide_console, is_reparse_point, remove_readonly};
use serde::Deserialize;
use serde_json::{json, Value};
use std::io::{BufRead, BufReader, Read, Write};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};
use walkdir::WalkDir;

#[derive(Debug, Clone, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct CompressionParams {
    #[serde(default = "default_level")]
    pub level: i32,
    #[serde(default)]
    pub dictionary_mb: Option<u32>,
    #[serde(default)]
    pub threads: Option<u32>,
    #[serde(default)]
    pub extra_args: Vec<String>,
}

fn default_level() -> i32 {
    5
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct BackupRequest {
    pub seven_zip: PathBuf,
    pub staging_dir: PathBuf,
    pub output: PathBuf,
    #[serde(default)]
    pub password: Option<String>,
    #[serde(default)]
    pub compression: CompressionParams,
    #[serde(default)]
    pub total_bytes: Option<u64>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExtractRequest {
    pub seven_zip: PathBuf,
    pub archive: PathBuf,
    pub destination: PathBuf,
    #[serde(default)]
    pub password: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TestRequest {
    pub seven_zip: PathBuf,
    pub archive: PathBuf,
    #[serde(default)]
    pub password: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ListRequest {
    pub seven_zip: PathBuf,
    pub archive: PathBuf,
    #[serde(default)]
    pub password: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CheckRequest {
    pub path: PathBuf,
}

pub fn check(request: CheckRequest) -> Result<Value> {
    if !request.path.exists() {
        return Err(SidecarError::new(format!(
            "找不到 7za.exe：{}",
            request.path.display()
        )));
    }
    let (sha256, size) = hash_file(&request.path)?;
    let output = run_capture(&request.path, &["i".to_string()])?;
    let version = output
        .lines()
        .find(|line| line.trim_start().starts_with("7-Zip"))
        .map(|line| line.trim().to_string())
        .unwrap_or_else(|| "7-Zip (未知版本)".to_string());
    Ok(json!({
        "path": request.path.to_string_lossy(),
        "sha256": sha256,
        "size": size,
        "version": version,
    }))
}

pub fn backup(request: BackupRequest, token: &AtomicBool, emitter: &Emitter) -> Result<Value> {
    ensure_dir(&request.staging_dir)?;
    if !request.staging_dir.is_dir() {
        return Err(SidecarError::new("暂存目录不存在"));
    }
    let mut list_entries: Vec<String> = Vec::new();
    for entry in WalkDir::new(&request.staging_dir)
        .follow_links(false)
        .into_iter()
        .filter_map(std::result::Result::ok)
    {
        if entry.depth() == 0 {
            continue;
        }
        let meta = entry.metadata().map_err(|error| {
            SidecarError::new(format!(
                "无法读取暂存目录项目 {}：{error}",
                entry.path().display()
            ))
        })?;
        if is_reparse_point(&meta) {
            return Err(SidecarError::new(format!(
                "暂存目录中出现链接，已拒绝压缩：{}",
                entry.path().display()
            )));
        }
        let relative = entry
            .path()
            .strip_prefix(&request.staging_dir)
            .map_err(|_| SidecarError::new("无法计算归档相对路径"))?;
        let normalized = relative.to_string_lossy().replace('/', "\\");
        validate_archive_path(&normalized)?;
        if entry.file_type().is_dir() {
            let mut children = std::fs::read_dir(entry.path()).map_err(|error| {
                SidecarError::new(format!(
                    "无法读取暂存目录 {}：{error}",
                    entry.path().display()
                ))
            })?;
            if children.next().is_none() {
                list_entries.push(normalized);
            }
            continue;
        }
        if !meta.is_file() {
            return Err(SidecarError::new(format!(
                "暂存目录中出现不支持的文件类型：{}",
                entry.path().display()
            )));
        }
        list_entries.push(normalized);
    }
    if list_entries.is_empty() {
        return Err(SidecarError::new("没有可压缩的文件"));
    }
    list_entries.sort();

    let list_path = request.output.with_extension("files.list.txt");
    if let Some(parent) = request.output.parent() {
        ensure_dir(parent)?;
    }
    if request.output.exists() {
        remove_readonly(&request.output);
        std::fs::remove_file(&request.output)?;
    }
    {
        let mut file = std::fs::File::create(&list_path)?;
        for entry in &list_entries {
            writeln!(file, "{}", entry)?;
        }
        file.sync_all().ok();
    }

    let mut args: Vec<String> = vec![
        "a".into(),
        "-t7z".into(),
        request.output.to_string_lossy().to_string(),
        format!("@{}", list_path.to_string_lossy()),
        format!("-mx={}", request.compression.level.clamp(0, 9)),
        "-ms=on".into(),
        "-scsUTF-8".into(),
        "-sccUTF-8".into(),
        "-bsp1".into(),
        "-bso1".into(),
        "-y".into(),
    ];
    if let Some(dictionary) = request.compression.dictionary_mb {
        validate_dictionary(dictionary)?;
        args.push(format!("-md={dictionary}m"));
    }
    if let Some(threads) = request.compression.threads {
        if !(1..=64).contains(&threads) {
            return Err(SidecarError::new("线程数超出允许范围"));
        }
        args.push(format!("-mmt={threads}"));
    }
    for extra in validate_extra_args(&request.compression.extra_args)? {
        args.push(extra);
    }
    if let Some(password) = request.password.as_ref().filter(|value| !value.is_empty()) {
        args.push(format!("-p{password}"));
        args.push("-mhe=on".into());
    }

    emitter.progress("compress", 0.0, "开始压缩", Some(0), request.total_bytes);
    let outcome = run_streaming(
        &request.seven_zip,
        &args,
        Some(&request.staging_dir),
        token,
        emitter,
        "compress",
        10.0,
        98.0,
        request.total_bytes,
    )?;

    let _ = std::fs::remove_file(&list_path);
    if !request.output.exists() {
        return Err(SidecarError::new("7z 未生成归档文件"));
    }
    let (sha256, size) = hash_file(&request.output)?;
    emitter.progress("compress", 100.0, "压缩完成", Some(size), Some(size));
    Ok(json!({
        "size": size,
        "sha256": sha256,
        "warnings": outcome.warnings,
        "exitCode": outcome.exit_code,
        "readBytes": outcome.last_bytes,
    }))
}

pub fn list(request: ListRequest) -> Result<Value> {
    let mut args = vec![
        "l".to_string(),
        "-slt".to_string(),
        "-sccUTF-8".to_string(),
        "-y".to_string(),
        request.archive.to_string_lossy().to_string(),
    ];
    if let Some(password) = request.password.as_ref().filter(|value| !value.is_empty()) {
        args.insert(1, format!("-p{password}"));
    }
    let output = run_capture(&request.seven_zip, &args)?;
    parse_list_output(&output)
}

pub fn validate_archive(seven_zip: &Path, archive: &Path, password: Option<&str>) -> Result<Value> {
    let mut args = vec![
        "l".to_string(),
        "-slt".to_string(),
        "-sccUTF-8".to_string(),
        "-y".to_string(),
        archive.to_string_lossy().to_string(),
    ];
    if let Some(value) = password.filter(|value| !value.is_empty()) {
        args.insert(1, format!("-p{value}"));
    }
    let output = run_capture(seven_zip, &args)?;
    let parsed = parse_list_output(&output)?;
    let entries = parsed["entries"]
        .as_array()
        .ok_or_else(|| SidecarError::new("无法解析归档列表"))?;
    for entry in entries {
        let path = entry
            .as_str()
            .ok_or_else(|| SidecarError::new("归档条目格式无效"))?;
        validate_archive_path(path)?;
    }
    Ok(parsed)
}

pub fn test(request: TestRequest, token: &AtomicBool, emitter: &Emitter) -> Result<Value> {
    let mut args = vec![
        "t".to_string(),
        "-sccUTF-8".to_string(),
        "-bsp1".to_string(),
        "-bso1".to_string(),
        "-y".to_string(),
        request.archive.to_string_lossy().to_string(),
    ];
    if let Some(password) = request.password.as_ref().filter(|value| !value.is_empty()) {
        args.insert(1, format!("-p{password}"));
    }
    emitter.progress("verify", 0.0, "开始完整性校验", Some(0), None);
    let outcome = run_streaming(
        &request.seven_zip,
        &args,
        None,
        token,
        emitter,
        "verify",
        0.0,
        100.0,
        None,
    )?;
    emitter.progress("verify", 100.0, "完整性校验完成", None, None);
    Ok(json!({
        "ok": outcome.exit_code == 0,
        "exitCode": outcome.exit_code,
        "warnings": outcome.warnings,
    }))
}

pub fn extract(request: ExtractRequest, token: &AtomicBool, emitter: &Emitter) -> Result<Value> {
    ensure_dir(&request.destination)?;
    if std::fs::read_dir(&request.destination)?.next().is_some() {
        return Err(SidecarError::new("解压目标目录不为空"));
    }

    emitter.progress("extract", 0.0, "正在检查归档内容", Some(0), None);
    let parsed = validate_archive(
        &request.seven_zip,
        &request.archive,
        request.password.as_deref(),
    )?;
    let entries = parsed["entries"]
        .as_array()
        .ok_or_else(|| SidecarError::new("无法解析归档列表"))?;
    let total_files = entries.len() as u64;
    let total_bytes = parsed["bytes"].as_u64().unwrap_or(0);

    let mut args = vec![
        "x".to_string(),
        request.archive.to_string_lossy().to_string(),
        format!("-o{}", request.destination.to_string_lossy()),
        "-sccUTF-8".to_string(),
        "-bsp1".to_string(),
        "-bso1".to_string(),
        "-y".to_string(),
    ];
    if let Some(password) = request.password.as_ref().filter(|value| !value.is_empty()) {
        args.insert(1, format!("-p{password}"));
    }

    emitter.progress(
        "extract",
        1.0,
        &format!("解压 {total_files} 个条目"),
        Some(0),
        Some(total_bytes),
    );
    let outcome = run_streaming(
        &request.seven_zip,
        &args,
        Some(&request.destination),
        token,
        emitter,
        "extract",
        2.0,
        95.0,
        Some(total_bytes),
    )?;

    // 解压后复核：任何链接/重解析点、遍历错误或越界路径都立即失败。
    for entry in WalkDir::new(&request.destination).follow_links(false) {
        let entry =
            entry.map_err(|error| SidecarError::new(format!("无法检查解压结果：{error}")))?;
        if entry.depth() == 0 {
            continue;
        }
        let meta = entry.metadata().map_err(|error| {
            SidecarError::new(format!(
                "无法读取解压结果 {}：{error}",
                entry.path().display()
            ))
        })?;
        if is_reparse_point(&meta) {
            let _ = std::fs::remove_dir_all(&request.destination);
            return Err(SidecarError::new(format!(
                "解压结果包含链接或重解析点：{}",
                entry.path().display()
            )));
        }
        if entry.file_type().is_file() || entry.file_type().is_dir() {
            ensure_inside(&request.destination, entry.path())?;
        }
    }

    emitter.progress(
        "extract",
        100.0,
        "解压完成",
        Some(total_bytes),
        Some(total_bytes),
    );
    Ok(json!({
        "files": total_files,
        "bytes": total_bytes,
        "warnings": outcome.warnings,
        "exitCode": outcome.exit_code,
        "entries": parsed["entries"],
    }))
}

fn parse_list_output(output: &str) -> Result<Value> {
    const MAX_ENTRIES: usize = 1_000_000;
    const MAX_PATH_BYTES: usize = 256 * 1024 * 1024;

    let mut entries: Vec<String> = Vec::new();
    let mut details: Vec<Value> = Vec::new();
    let mut seen = std::collections::HashSet::new();
    let mut total_bytes = 0u64;
    let mut path_bytes = 0usize;
    let marker = "----------";
    let mut in_entries = false;
    let mut current_path: Option<String> = None;
    let mut current_size: Option<u64> = None;
    let mut current_folder = false;

    let mut flush =
        |path: &mut Option<String>, size: &mut Option<u64>, folder: &mut bool| -> Result<()> {
            if let Some(value) = path.take() {
                let normalized = validate_archive_path(&value)?;
                let identity = normalized.to_ascii_lowercase();
                if !seen.insert(identity) {
                    return Err(SidecarError::new(format!(
                        "归档包含重复或大小写冲突的路径：{normalized}"
                    )));
                }
                let size_value = size.take().unwrap_or(0);
                if !*folder {
                    total_bytes = total_bytes
                        .checked_add(size_value)
                        .ok_or_else(|| SidecarError::new("归档声明的总大小溢出"))?;
                }
                path_bytes = path_bytes
                    .checked_add(normalized.len())
                    .ok_or_else(|| SidecarError::new("归档路径总量溢出"))?;
                if path_bytes > MAX_PATH_BYTES {
                    return Err(SidecarError::new("归档路径总量超出处理上限"));
                }
                if entries.len() >= MAX_ENTRIES {
                    return Err(SidecarError::new("归档条目数量超出处理上限"));
                }
                entries.push(normalized.clone());
                details.push(json!({
                    "path": normalized,
                    "size": if *folder { 0 } else { size_value },
                    "folder": *folder,
                }));
            }
            *size = None;
            *folder = false;
            Ok(())
        };

    for line in output.lines() {
        let trimmed = line.trim_end_matches('\r');
        if trimmed.trim() == marker {
            in_entries = true;
            continue;
        }
        if !in_entries {
            continue;
        }
        if trimmed.trim().is_empty() {
            flush(&mut current_path, &mut current_size, &mut current_folder)?;
            continue;
        }
        if let Some(value) = trimmed.strip_prefix("Path = ") {
            flush(&mut current_path, &mut current_size, &mut current_folder)?;
            current_path = Some(value.to_string());
        } else if let Some(value) = trimmed.strip_prefix("Size = ") {
            let parsed = value
                .trim()
                .parse::<u64>()
                .map_err(|_| SidecarError::new(format!("归档条目大小无效：{}", value.trim())))?;
            current_size = Some(parsed);
        } else if let Some(value) = trimmed.strip_prefix("Folder = ") {
            current_folder = value.trim() == "+";
        } else if let Some(value) = trimmed.strip_prefix("Attributes = ") {
            if value.split_whitespace().any(|part| part == "D") {
                current_folder = true;
            }
        }
    }
    flush(&mut current_path, &mut current_size, &mut current_folder)?;

    if entries.is_empty() {
        return Err(SidecarError::new(
            "归档为空、已损坏，或密码错误。请确认归档和本地私钥。",
        ));
    }
    Ok(json!({
        "entries": entries,
        "details": details,
        "files": entries.len(),
        "bytes": total_bytes,
    }))
}

fn validate_dictionary(value: u32) -> Result<()> {
    const ALLOWED: [u32; 7] = [16, 32, 64, 128, 256, 512, 1024];
    if ALLOWED.contains(&value) {
        Ok(())
    } else {
        Err(SidecarError::new(
            "字典大小只允许 16/32/64/128/256/512/1024 MB",
        ))
    }
}

pub fn validate_extra_args(args: &[String]) -> Result<Vec<String>> {
    if args.len() > 12 {
        return Err(SidecarError::new("高级参数过多"));
    }
    let mut result = Vec::new();
    for arg in args {
        let trimmed = arg.trim();
        if trimmed.is_empty() {
            continue;
        }
        if trimmed.len() > 64 {
            return Err(SidecarError::new(format!("高级参数过长：{trimmed}")));
        }
        let allowed = if let Some(value) = trimmed.strip_prefix("-md=") {
            parse_dictionary(value).is_ok()
        } else if let Some(value) = trimmed.strip_prefix("-mmt=") {
            value
                .parse::<u8>()
                .map(|value| (1..=64).contains(&value))
                .unwrap_or(false)
        } else if let Some(value) = trimmed.strip_prefix("-mfb=") {
            value
                .parse::<u16>()
                .map(|value| (5..=273).contains(&value))
                .unwrap_or(false)
        } else if let Some(value) = trimmed.strip_prefix("-ms=") {
            value == "on" || value == "off"
        } else if let Some(value) = trimmed.strip_prefix("-mqs=") {
            value == "on" || value == "off"
        } else {
            false
        };
        if !allowed {
            return Err(SidecarError::new(format!(
                "不允许的 7z 高级参数：{trimmed}"
            )));
        }
        result.push(trimmed.to_string());
    }
    Ok(result)
}

fn parse_dictionary(value: &str) -> Result<u32> {
    let lower = value.to_ascii_lowercase();
    let (number, scale) = if let Some(number) = lower.strip_suffix('m') {
        (number, 1u32)
    } else if let Some(number) = lower.strip_suffix('g') {
        (number, 1024u32)
    } else {
        return Err(SidecarError::new("字典大小格式无效"));
    };
    let parsed = number
        .parse::<u32>()
        .map_err(|_| SidecarError::new("字典大小格式无效"))?;
    let megabytes = parsed
        .checked_mul(scale)
        .ok_or_else(|| SidecarError::new("字典大小过大"))?;
    validate_dictionary(megabytes)?;
    Ok(megabytes)
}

struct Outcome {
    exit_code: i32,
    warnings: Vec<String>,
    last_bytes: Option<u64>,
}

#[allow(clippy::too_many_arguments)]
fn run_streaming(
    executable: &Path,
    args: &[String],
    cwd: Option<&Path>,
    token: &AtomicBool,
    emitter: &Emitter,
    phase: &str,
    start_percent: f64,
    end_percent: f64,
    total_bytes: Option<u64>,
) -> Result<Outcome> {
    if !executable.exists() {
        return Err(SidecarError::new(format!(
            "找不到 7za.exe：{}",
            executable.display()
        )));
    }
    let mut command = Command::new(executable);
    command
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    if let Some(dir) = cwd {
        command.current_dir(dir);
    }
    hide_console(&mut command);
    let mut child = command
        .spawn()
        .map_err(|error| SidecarError::new(format!("无法启动 7z 进程：{error}")))?;

    let stdout = child
        .stdout
        .take()
        .ok_or_else(|| SidecarError::new("无法读取 7z 输出"))?;
    let stderr = child
        .stderr
        .take()
        .ok_or_else(|| SidecarError::new("无法读取 7z 错误输出"))?;

    let emitter_clone = emitter.clone();
    let phase_owned = phase.to_string();
    let last_bytes = Arc::new(Mutex::new(None::<u64>));
    let last_bytes_writer = last_bytes.clone();
    let span = (end_percent - start_percent).max(0.1);
    let stdout_thread = thread::spawn(move || {
        let mut reader = stdout;
        let mut pending: Vec<u8> = Vec::new();
        let mut chunk = [0u8; 4096];
        let mut last_emit = Instant::now() - Duration::from_secs(1);
        let mut last_percent = -1.0f64;
        let mut emit = |segment: &str| {
            if let Some((percent, message)) = parse_progress_segment(segment) {
                let mapped = start_percent + (percent / 100.0) * span;
                if last_emit.elapsed() >= Duration::from_millis(150)
                    || (mapped - last_percent).abs() >= 1.0
                {
                    last_emit = Instant::now();
                    last_percent = mapped;
                    let bytes = total_bytes.map(|total| ((percent / 100.0) * total as f64) as u64);
                    if let Ok(mut guard) = last_bytes_writer.lock() {
                        *guard = bytes;
                    }
                    emitter_clone.progress(
                        &phase_owned,
                        mapped,
                        message.as_deref().unwrap_or("处理中"),
                        bytes,
                        total_bytes,
                    );
                }
            }
        };
        loop {
            match reader.read(&mut chunk) {
                Ok(0) => break,
                Ok(read) => {
                    pending.extend_from_slice(&chunk[..read]);
                    loop {
                        let separator = pending
                            .iter()
                            .position(|byte| *byte == b'\r' || *byte == b'\n');
                        let Some(position) = separator else { break };
                        let segment = String::from_utf8_lossy(&pending[..position]).to_string();
                        pending.drain(..=position);
                        emit(&segment);
                    }
                }
                Err(_) => break,
            }
        }
        if !pending.is_empty() {
            let tail = String::from_utf8_lossy(&pending).to_string();
            emit(&tail);
        }
    });

    let stderr_thread = thread::spawn(move || {
        let mut reader = BufReader::new(stderr);
        let mut tail: Vec<u8> = Vec::new();
        let mut buffer: Vec<u8> = Vec::new();
        loop {
            buffer.clear();
            match reader.read_until(b'\n', &mut buffer) {
                Ok(0) => break,
                Ok(_) => {
                    tail.extend_from_slice(&buffer);
                    if tail.len() > 16 * 1024 {
                        let keep = tail.len() - 12 * 1024;
                        tail.drain(..keep);
                    }
                }
                Err(_) => break,
            }
        }
        String::from_utf8_lossy(&tail).into_owned()
    });

    let mut cancelled = false;
    let status = loop {
        if token.load(Ordering::Relaxed) {
            cancelled = true;
            let _ = child.kill();
        }
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) => thread::sleep(Duration::from_millis(100)),
            Err(error) => {
                let _ = child.kill();
                return Err(SidecarError::new(format!("等待 7z 进程失败：{error}")));
            }
        }
    };

    let _ = stdout_thread.join();
    let stderr_tail = stderr_thread.join().unwrap_or_default();
    if cancelled {
        return Err(SidecarError::cancelled());
    }

    let exit_code = status.code().unwrap_or(-1);
    let mut warnings = Vec::new();
    for line in stderr_tail.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        if trimmed.starts_with("WARNING:") || trimmed.starts_with("Scan WARNINGS") {
            warnings.push(trimmed.to_string());
        }
    }
    if exit_code == 1 {
        warnings.push("7z 报告了非致命警告，归档已生成。".to_string());
    }
    if exit_code != 0 && exit_code != 1 {
        let summary = summarize_7z_error(exit_code, &stderr_tail);
        return Err(SidecarError::with_data(
            summary,
            json!({ "exitCode": exit_code, "stderr": tail_lines(&stderr_tail, 40) }),
        ));
    }

    let last = last_bytes.lock().ok().and_then(|guard| *guard);
    Ok(Outcome {
        exit_code,
        warnings,
        last_bytes: last,
    })
}

fn summarize_7z_error(exit_code: i32, stderr: &str) -> String {
    let mapped = match exit_code {
        2 => "7z 发生致命错误，归档未完成",
        7 => "7z 命令行参数错误",
        8 => "内存不足，7z 无法完成压缩",
        255 => "7z 进程被终止",
        -1 => "7z 进程异常退出",
        _ => "7z 执行失败",
    };
    let detail = tail_lines(stderr, 4)
        .lines()
        .filter(|line| !line.trim().is_empty())
        .collect::<Vec<_>>()
        .join("；");
    if detail.is_empty() {
        format!("{mapped}（退出码 {exit_code}）")
    } else {
        format!("{mapped}（退出码 {exit_code}）：{detail}")
    }
}

fn tail_lines(value: &str, count: usize) -> String {
    let lines: Vec<&str> = value.lines().collect();
    let start = lines.len().saturating_sub(count);
    lines[start..].join("\n")
}

fn parse_progress_segment(segment: &str) -> Option<(f64, Option<String>)> {
    let percent_index = segment.find('%')?;
    let prefix = &segment[..percent_index];
    let digits_start = prefix
        .rfind(|ch: char| !ch.is_ascii_digit())
        .map(|index| index + ch_len(prefix, index))
        .unwrap_or(0);
    let digits = prefix.get(digits_start..)?.trim();
    if digits.is_empty() {
        return None;
    }
    let percent = digits.parse::<f64>().ok()?;
    if !(0.0..=100.0).contains(&percent) {
        return None;
    }
    let message = segment
        .split_once(" - ")
        .map(|(_, value)| value.trim().to_string())
        .filter(|value| !value.is_empty());
    Some((percent, message))
}

fn ch_len(value: &str, byte_index: usize) -> usize {
    value[byte_index..]
        .chars()
        .next()
        .map(|ch| ch.len_utf8())
        .unwrap_or(0)
}

fn run_capture(executable: &Path, args: &[String]) -> Result<String> {
    if !executable.exists() {
        return Err(SidecarError::new(format!(
            "找不到 7za.exe：{}",
            executable.display()
        )));
    }
    let mut command = Command::new(executable);
    command
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    hide_console(&mut command);
    let output = command
        .output()
        .map_err(|error| SidecarError::new(format!("无法运行 7z：{error}")))?;
    let stdout = String::from_utf8_lossy(&output.stdout).to_string();
    let stderr = String::from_utf8_lossy(&output.stderr).to_string();
    if !output.status.success() {
        let code = output.status.code().unwrap_or(-1);
        return Err(SidecarError::with_data(
            summarize_7z_error(code, &stderr),
            json!({ "exitCode": code, "stderr": tail_lines(&stderr, 30) }),
        ));
    }
    Ok(stdout)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn extra_args_whitelist() {
        assert!(validate_extra_args(&[
            "-mfb=273".into(),
            "-ms=on".into(),
            "-md=128m".into(),
            "-mmt=8".into()
        ])
        .is_ok());
        for arg in [
            "-psecret",
            "-oC:\\Windows",
            "@/tmp/list",
            "-wC:\\",
            "-mhe=off",
            "-mx=10",
            "-mx=0",
            "-mmt=0",
            "-md=7m",
            "-sdel",
        ] {
            assert!(validate_extra_args(&[arg.to_string()]).is_err(), "{arg}");
        }
    }

    #[test]
    fn parses_progress() {
        let parsed = parse_progress_segment(" 45% 12 - dsh\\sessions\\a.json").unwrap();
        assert_eq!(parsed.0, 45.0);
        assert_eq!(parsed.1.as_deref(), Some("dsh\\sessions\\a.json"));
        assert!(parse_progress_segment("no progress").is_none());
    }
}
