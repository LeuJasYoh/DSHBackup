use crate::error::{Result, SidecarError};
use sha2::{Digest, Sha256};
use std::fs::File;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};

pub fn hash_file(path: &Path) -> Result<(String, u64)> {
    let mut file = File::open(path).map_err(|error| {
        SidecarError::new(format!("无法读取文件 {}：{error}", path.display()))
    })?;
    let mut hasher = Sha256::new();
    let mut buffer = vec![0u8; 1024 * 256];
    let mut size = 0u64;
    loop {
        let read = file.read(&mut buffer)?;
        if read == 0 {
            break;
        }
        size += read as u64;
        hasher.update(&buffer[..read]);
    }
    Ok((hex::encode(hasher.finalize()), size))
}


#[cfg(windows)]
pub fn is_reparse_point(meta: &std::fs::Metadata) -> bool {
    use std::os::windows::fs::MetadataExt;
    const FILE_ATTRIBUTE_REPARSE_POINT: u32 = 0x0400;
    meta.file_attributes() & FILE_ATTRIBUTE_REPARSE_POINT != 0
}

#[cfg(not(windows))]
pub fn is_reparse_point(meta: &std::fs::Metadata) -> bool {
    meta.file_type().is_symlink()
}


pub fn remove_readonly(path: &Path) {
    if let Ok(meta) = std::fs::metadata(path) {
        let mut permissions = meta.permissions();
        if permissions.readonly() {
            #[allow(clippy::permissions_set_readonly_false)]
            permissions.set_readonly(false);
            let _ = std::fs::set_permissions(path, permissions);
        }
    }
}

pub fn copy_file_preserve(src: &Path, dst: &Path) -> Result<()> {
    if let Some(parent) = dst.parent() {
        std::fs::create_dir_all(parent)?;
    }
    remove_readonly(dst);
    std::fs::copy(src, dst).map_err(|error| {
        SidecarError::new(format!(
            "复制文件失败 {} -> {}：{error}",
            src.display(),
            dst.display()
        ))
    })?;
    if let Ok(meta) = std::fs::metadata(src) {
        let mtime = filetime::FileTime::from_last_modification_time(&meta);
        let atime = filetime::FileTime::from_last_access_time(&meta);
        let _ = filetime::set_file_times(dst, atime, mtime);
    }
    Ok(())
}

pub fn link_or_copy(src: &Path, dst: &Path) -> Result<bool> {
    if let Some(parent) = dst.parent() {
        std::fs::create_dir_all(parent)?;
    }
    remove_readonly(dst);
    if std::fs::hard_link(src, dst).is_ok() {
        return Ok(true);
    }
    std::fs::copy(src, dst).map_err(|error| {
        SidecarError::new(format!(
            "复制文件失败 {} -> {}：{error}",
            src.display(),
            dst.display()
        ))
    })?;
    if let Ok(meta) = std::fs::metadata(src) {
        let mtime = filetime::FileTime::from_last_modification_time(&meta);
        let atime = filetime::FileTime::from_last_access_time(&meta);
        let _ = filetime::set_file_times(dst, atime, mtime);
    }
    Ok(false)
}

pub fn ensure_dir(path: &Path) -> Result<()> {
    std::fs::create_dir_all(path).map_err(|error| {
        SidecarError::new(format!("无法创建目录 {}：{error}", path.display()))
    })
}

pub fn write_atomic(path: &Path, bytes: &[u8]) -> Result<()> {
    if let Some(parent) = path.parent() {
        ensure_dir(parent)?;
    }
    let temp = path.with_extension(format!(
        "{}.tmp",
        path.extension()
            .and_then(|value| value.to_str())
            .unwrap_or("file")
    ));
    {
        let mut file = File::create(&temp)?;
        file.write_all(bytes)?;
        file.sync_all().ok();
    }
    if path.exists() {
        remove_readonly(path);
        std::fs::remove_file(path)?;
    }
    std::fs::rename(&temp, path)?;
    Ok(())
}


pub fn canonical(path: &Path) -> Result<PathBuf> {
    std::fs::canonicalize(path).map_err(|error| {
        SidecarError::new(format!("无法解析路径 {}：{error}", path.display()))
    })
}

#[cfg(windows)]
pub fn hide_console(command: &mut std::process::Command) {
    use std::os::windows::process::CommandExt;
    const CREATE_NO_WINDOW: u32 = 0x0800_0000;
    command.creation_flags(CREATE_NO_WINDOW);
}

#[cfg(not(windows))]
pub fn hide_console(_command: &mut std::process::Command) {}
