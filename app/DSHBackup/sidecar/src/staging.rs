use crate::error::{Result, SidecarError};
use crate::protocol::Emitter;
use crate::util::{
    ensure_dir, ensure_no_reparse_chain, is_reparse_point, replace_file, stable_copy,
};
use serde::Deserialize;
use serde_json::{json, Value};
use std::collections::HashSet;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant};
use walkdir::WalkDir;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StageRoot {
    pub name: String,
    pub source: PathBuf,
    #[serde(default)]
    pub exclude_top: Vec<String>,
    #[serde(default)]
    pub exclude_rel: Vec<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StageRequest {
    pub staging_dir: PathBuf,
    pub roots: Vec<StageRoot>,
}

struct PlannedFile {
    source: PathBuf,
    relative: PathBuf,
    size: u64,
}

struct PlannedDir {
    relative: PathBuf,
}

fn slash_path(path: &Path) -> String {
    path.to_string_lossy().replace('\\', "/")
}

pub fn stage(request: StageRequest, token: &AtomicBool, emitter: &Emitter) -> Result<Value> {
    ensure_dir(&request.staging_dir)?;
    if std::fs::read_dir(&request.staging_dir)?.next().is_some() {
        return Err(SidecarError::new("暂存目录不是空目录，已拒绝写入"));
    }

    let mut files: Vec<PlannedFile> = Vec::new();
    let mut dirs: Vec<PlannedDir> = Vec::new();
    let mut warnings: Vec<String> = Vec::new();
    let mut total_bytes = 0u64;
    let mut skipped = 0usize;

    emitter.progress("stage", 0.0, "正在扫描源文件", Some(0), None);

    for root in &request.roots {
        if !root.name.is_empty() && root.name != "dsh" && root.name != "desktop-state" {
            return Err(SidecarError::new(format!(
                "不支持的备份范围：{}",
                root.name
            )));
        }
        let source_meta = match std::fs::symlink_metadata(&root.source) {
            Ok(value) => value,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                return Err(SidecarError::new(format!(
                    "备份源不存在：{}",
                    root.source.display()
                )))
            }
            Err(error) => {
                return Err(SidecarError::new(format!(
                    "无法读取备份源 {}：{error}",
                    root.source.display()
                )))
            }
        };
        if !source_meta.is_dir() {
            return Err(SidecarError::new(format!(
                "备份源不是目录：{}",
                root.source.display()
            )));
        }

        let exclude_top: HashSet<String> = root
            .exclude_top
            .iter()
            .map(|value| value.to_ascii_lowercase())
            .collect();
        let exclude_rel: HashSet<String> = root
            .exclude_rel
            .iter()
            .map(|value| value.replace('\\', "/").to_ascii_lowercase())
            .collect();

        let mut filtered_reparse = 0usize;
        let walker = WalkDir::new(&root.source)
            .follow_links(false)
            .into_iter()
            .filter_entry(|entry| {
                if entry.depth() == 0 {
                    return true;
                }
                let relative = match entry.path().strip_prefix(&root.source) {
                    Ok(value) => value,
                    Err(_) => return false,
                };
                let mut parts = relative.components();
                if let Some(first) = parts.next() {
                    if entry.depth() == 1 {
                        let name = first.as_os_str().to_string_lossy().to_ascii_lowercase();
                        if exclude_top.contains(&name) {
                            return false;
                        }
                    }
                }
                let normalized = relative
                    .to_string_lossy()
                    .replace('\\', "/")
                    .to_ascii_lowercase();
                if exclude_rel.contains(&normalized) {
                    return false;
                }
                match entry.metadata() {
                    Ok(meta) if is_reparse_point(&meta) => {
                        filtered_reparse += 1;
                        false
                    }
                    Ok(_) => true,
                    Err(_) => true,
                }
            });

        for entry in walker {
            if token.load(Ordering::Relaxed) {
                return Err(SidecarError::cancelled());
            }
            let entry = match entry {
                Ok(value) => value,
                Err(error) => {
                    warnings.push(format!("扫描项目失败：{error}"));
                    skipped += 1;
                    continue;
                }
            };
            if entry.depth() == 0 {
                continue;
            }
            let relative = entry
                .path()
                .strip_prefix(&root.source)
                .map_err(|_| SidecarError::new("无法计算相对路径"))?
                .to_path_buf();
            let named_relative = if root.name.is_empty() {
                relative
            } else {
                PathBuf::from(&root.name).join(&relative)
            };
            let meta = match entry.metadata() {
                Ok(value) => value,
                Err(error) => {
                    warnings.push(format!(
                        "无法读取文件信息 {}：{error}",
                        named_relative.display()
                    ));
                    skipped += 1;
                    continue;
                }
            };
            if is_reparse_point(&meta) {
                warnings.push(format!(
                    "已跳过链接或重解析点：{}",
                    named_relative.display()
                ));
                skipped += 1;
                continue;
            }
            if meta.is_dir() {
                dirs.push(PlannedDir {
                    relative: named_relative,
                });
                continue;
            }
            if !meta.is_file() {
                skipped += 1;
                continue;
            }
            total_bytes += meta.len();
            files.push(PlannedFile {
                source: entry.path().to_path_buf(),
                relative: named_relative,
                size: meta.len(),
            });
        }
        if filtered_reparse > 0 {
            warnings.push(format!(
                "备份源中跳过了 {filtered_reparse} 个链接或重解析点"
            ));
            skipped += filtered_reparse;
        }
    }

    emitter.progress(
        "stage",
        2.0,
        &format!("准备写入 {} 个文件、{} 个目录", files.len(), dirs.len()),
        Some(0),
        Some(total_bytes),
    );

    let mut staged_bytes = 0u64;
    let mut staged_files = 0u64;
    let mut last_emit = Instant::now() - Duration::from_secs(1);
    let mut last_percent = 0.0f64;

    for dir in &dirs {
        let destination = request.staging_dir.join(&dir.relative);
        if let Err(error) = ensure_dir(&destination) {
            warnings.push(format!(
                "目录未能暂存：{}（{error}）",
                dir.relative.display()
            ));
            skipped += 1;
        }
    }

    for file in &files {
        if token.load(Ordering::Relaxed) {
            return Err(SidecarError::cancelled());
        }
        let destination = request.staging_dir.join(&file.relative);
        match stable_copy(&file.source, &destination, token) {
            Ok(size) if size == file.size => {
                staged_bytes += size;
                staged_files += 1;
            }
            Ok(size) => {
                let _ = std::fs::remove_file(&destination);
                warnings.push(format!(
                    "文件在暂存期间发生变化（预期 {} 字节，实际 {} 字节），已跳过：{}",
                    file.size,
                    size,
                    file.relative.display()
                ));
                skipped += 1;
            }
            Err(error) => {
                warnings.push(format!(
                    "文件被占用或无法读取，已跳过：{}（{error}）",
                    file.relative.display()
                ));
                skipped += 1;
            }
        }

        let percent = if total_bytes == 0 {
            100.0
        } else {
            (staged_bytes as f64 / total_bytes as f64) * 100.0
        };
        if last_emit.elapsed() >= Duration::from_millis(150) || percent - last_percent >= 5.0 {
            last_emit = Instant::now();
            last_percent = percent;
            emitter.progress(
                "stage",
                2.0 + percent * 0.08,
                &format!("暂存 {}", file.relative.display()),
                Some(staged_bytes),
                Some(total_bytes),
            );
        }
    }

    for entry in WalkDir::new(&request.staging_dir)
        .follow_links(false)
        .into_iter()
        .filter_map(std::result::Result::ok)
    {
        if entry.depth() == 0 {
            continue;
        }
        if let Ok(meta) = entry.metadata() {
            if is_reparse_point(&meta) {
                let _ = std::fs::remove_dir_all(&request.staging_dir);
                return Err(SidecarError::new(format!(
                    "暂存目录中检测到链接或重解析点：{}",
                    entry.path().display()
                )));
            }
        }
    }

    let mut entries: Vec<Value> = Vec::new();
    for dir in &dirs {
        if request.staging_dir.join(&dir.relative).is_dir() {
            entries.push(json!({
                "path": slash_path(&dir.relative),
                "size": 0,
                "folder": true,
            }));
        }
    }
    for file in &files {
        let destination = request.staging_dir.join(&file.relative);
        if let Ok(meta) = std::fs::metadata(&destination) {
            entries.push(json!({
                "path": slash_path(&file.relative),
                "size": meta.len(),
                "folder": false,
            }));
        }
    }
    entries.sort_by(|left, right| {
        left["path"]
            .as_str()
            .unwrap_or_default()
            .cmp(right["path"].as_str().unwrap_or_default())
    });

    emitter.progress(
        "stage",
        10.0,
        &format!("已暂存 {staged_files} 个文件"),
        Some(staged_bytes),
        Some(total_bytes),
    );

    Ok(json!({
        "files": staged_files,
        "directories": dirs.len(),
        "bytes": staged_bytes,
        "totalBytes": total_bytes,
        "hardlinked": 0,
        "skipped": skipped,
        "complete": skipped == 0 && warnings.is_empty(),
        "warnings": warnings,
        "entries": entries,
        "stagingDir": request.staging_dir.to_string_lossy(),
    }))
}

pub fn install_staging(
    staging_dir: &Path,
    targets: &[(String, PathBuf)],
    token: &AtomicBool,
    emitter: &Emitter,
) -> Result<Value> {
    let mut total_files = 0u64;
    let mut total_bytes = 0u64;
    for (name, _target) in targets {
        let root = staging_dir.join(name);
        if !root.exists() {
            continue;
        }
        for entry in WalkDir::new(&root).follow_links(false) {
            let entry = entry
                .map_err(|error| SidecarError::new(format!("无法扫描恢复暂存内容：{error}")))?;
            if entry.file_type().is_file() {
                let meta = entry.metadata().map_err(|error| {
                    SidecarError::new(format!(
                        "无法读取恢复暂存文件 {}：{error}",
                        entry.path().display()
                    ))
                })?;
                total_files += 1;
                total_bytes += meta.len();
            }
        }
    }

    let mut copied_files = 0u64;
    let mut copied_bytes = 0u64;
    let mut copied_dirs = 0u64;
    let mut last_emit = Instant::now() - Duration::from_secs(1);
    let warnings: Vec<String> = Vec::new();

    for (name, target) in targets {
        if name != "dsh" && name != "desktop-state" {
            return Err(SidecarError::new(format!("不支持的恢复范围：{name}")));
        }
        let root = staging_dir.join(name);
        if !root.exists() {
            continue;
        }
        if let Ok(meta) = std::fs::symlink_metadata(target) {
            if !is_reparse_point(&meta) && !meta.is_dir() {
                return Err(SidecarError::new(format!(
                    "恢复目标不是目录：{}",
                    target.display()
                )));
            }
        }
        ensure_dir(target)?;
        for entry in WalkDir::new(&root).follow_links(false) {
            if token.load(Ordering::Relaxed) {
                return Err(SidecarError::cancelled());
            }
            let entry = entry
                .map_err(|error| SidecarError::new(format!("无法扫描恢复暂存内容：{error}")))?;
            if entry.depth() == 0 {
                continue;
            }
            let meta = match entry.metadata() {
                Ok(value) => value,
                Err(error) => {
                    return Err(SidecarError::new(format!(
                        "无法读取暂存文件 {}：{error}",
                        entry.path().display()
                    )))
                }
            };
            if is_reparse_point(&meta) {
                return Err(SidecarError::new(format!(
                    "恢复暂存目录出现链接，已中止：{}",
                    entry.path().display()
                )));
            }
            let relative = entry
                .path()
                .strip_prefix(&root)
                .map_err(|_| SidecarError::new("无法计算恢复相对路径"))?;
            let destination = target.join(relative);

            if meta.is_dir() {
                ensure_no_reparse_chain(target, relative)?;
                ensure_dir(&destination)?;
                copied_dirs += 1;
                continue;
            } else if !meta.is_file() {
                return Err(SidecarError::new(format!(
                    "恢复暂存中出现不支持的文件类型：{}",
                    relative.display()
                )));
            }

            ensure_no_reparse_chain(target, relative.parent().unwrap_or(Path::new("")))?;
            match replace_file(entry.path(), &destination, token) {
                Ok(()) => {
                    copied_files += 1;
                    copied_bytes += meta.len();
                }
                Err(error) => {
                    return Err(SidecarError::new(format!(
                        "写入恢复文件失败：{}（{error}）",
                        relative.display()
                    )));
                }
            }
            if last_emit.elapsed() >= Duration::from_millis(150) {
                last_emit = Instant::now();
                let percent = if total_bytes == 0 {
                    100.0
                } else {
                    (copied_bytes as f64 / total_bytes as f64) * 100.0
                };
                emitter.progress(
                    "install",
                    percent,
                    &format!("写入 {}", relative.display()),
                    Some(copied_bytes),
                    Some(total_bytes),
                );
            }
        }
    }

    Ok(json!({
        "files": copied_files,
        "directories": copied_dirs,
        "bytes": copied_bytes,
        "totalFiles": total_files,
        "warnings": warnings,
    }))
}
