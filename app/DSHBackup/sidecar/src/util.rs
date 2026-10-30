use crate::error::{Result, SidecarError};
use sha2::{Digest, Sha256};
use std::fs::{File, OpenOptions};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};

static CANDIDATE_COUNTER: AtomicU64 = AtomicU64::new(1);

pub fn hash_file(path: &Path) -> Result<(String, u64)> {
    let mut file = File::open(path)
        .map_err(|error| SidecarError::new(format!("无法读取文件 {}：{error}", path.display())))?;
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

fn copy_stream(source: &mut File, destination: &mut File, token: &AtomicBool) -> Result<u64> {
    let mut buffer = vec![0u8; 1024 * 1024];
    let mut total = 0u64;
    loop {
        if token.load(Ordering::Relaxed) {
            return Err(SidecarError::cancelled());
        }
        let read = source.read(&mut buffer)?;
        if read == 0 {
            break;
        }
        destination.write_all(&buffer[..read])?;
        total += read as u64;
    }
    destination.sync_all()?;
    Ok(total)
}

/// Independent copy for staging. On Windows the source is opened without
/// write/delete sharing so a concurrent writer becomes an explicit error
/// instead of silently changing already staged content.
#[cfg(windows)]
pub fn stable_copy(source: &Path, destination: &Path, token: &AtomicBool) -> Result<u64> {
    use std::os::windows::io::{FromRawHandle, RawHandle};
    use std::ptr;
    use windows_sys::Win32::Foundation::{GENERIC_READ, INVALID_HANDLE_VALUE};
    use windows_sys::Win32::Storage::FileSystem::{
        CreateFileW, FILE_ATTRIBUTE_NORMAL, FILE_SHARE_READ, OPEN_EXISTING,
    };

    if let Some(parent) = destination.parent() {
        ensure_dir(parent)?;
    }
    let wide = wide_path(source);
    let handle = unsafe {
        CreateFileW(
            wide.as_ptr(),
            GENERIC_READ,
            FILE_SHARE_READ,
            ptr::null(),
            OPEN_EXISTING,
            FILE_ATTRIBUTE_NORMAL,
            ptr::null_mut(),
        )
    };
    if handle == INVALID_HANDLE_VALUE {
        return Err(SidecarError::new(format!(
            "无法以独占读取方式打开，文件可能仍被写入：{}",
            source.display()
        )));
    }
    let mut input = unsafe { File::from_raw_handle(handle as RawHandle) };
    let mut output = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(destination)
        .map_err(|error| {
            SidecarError::new(format!(
                "无法创建暂存文件 {}：{error}",
                destination.display()
            ))
        })?;
    let result = copy_stream(&mut input, &mut output, token);
    drop(output);
    if result.is_err() {
        let _ = std::fs::remove_file(destination);
    }
    result
}

#[cfg(not(windows))]
pub fn stable_copy(source: &Path, destination: &Path, token: &AtomicBool) -> Result<u64> {
    if let Some(parent) = destination.parent() {
        ensure_dir(parent)?;
    }
    let mut input = File::open(source)?;
    let mut output = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(destination)?;
    let result = copy_stream(&mut input, &mut output, token);
    if result.is_err() {
        let _ = std::fs::remove_file(destination);
    }
    result
}

fn candidate_path(destination: &Path) -> PathBuf {
    let name = destination
        .file_name()
        .and_then(|value| value.to_str())
        .unwrap_or("file");
    let nonce = CANDIDATE_COUNTER.fetch_add(1, Ordering::Relaxed);
    destination.with_file_name(format!(".{name}.dshbak-{}-{nonce}.tmp", std::process::id()))
}

#[cfg(windows)]
fn wide_path(path: &Path) -> Vec<u16> {
    use std::os::windows::ffi::OsStrExt;
    path.as_os_str().encode_wide().chain(Some(0)).collect()
}

#[cfg(windows)]
fn commit_candidate(candidate: &Path, destination: &Path, replace: bool) -> Result<()> {
    use std::mem::{offset_of, size_of};
    use std::ptr;
    use windows_sys::Win32::Foundation::{CloseHandle, GetLastError, INVALID_HANDLE_VALUE};
    use windows_sys::Win32::Storage::FileSystem::FILE_RENAME_INFO;
    use windows_sys::Win32::Storage::FileSystem::{
        CreateFileW, FileRenameInfoEx, SetFileInformationByHandle, DELETE, FILE_ATTRIBUTE_NORMAL,
        FILE_ATTRIBUTE_READONLY, FILE_SHARE_DELETE, FILE_SHARE_READ, FILE_SHARE_WRITE,
        FILE_WRITE_ATTRIBUTES, OPEN_EXISTING, SYNCHRONIZE,
    };

    const FILE_RENAME_REPLACE_IF_EXISTS: u32 = 0x0000_0001;
    const FILE_RENAME_IGNORE_READONLY_ATTRIBUTE: u32 = 0x0000_0040;

    let from = wide_path(candidate);
    let to = wide_path(destination);
    let handle = unsafe {
        CreateFileW(
            from.as_ptr(),
            DELETE | SYNCHRONIZE | FILE_WRITE_ATTRIBUTES,
            FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
            ptr::null(),
            OPEN_EXISTING,
            FILE_ATTRIBUTE_NORMAL,
            ptr::null_mut(),
        )
    };
    if handle == INVALID_HANDLE_VALUE {
        return Err(SidecarError::new(format!(
            "无法打开替换候选 {}：系统错误 {}",
            candidate.display(),
            unsafe { GetLastError() }
        )));
    }

    let mut flags = if replace {
        FILE_RENAME_REPLACE_IF_EXISTS
    } else {
        0
    };
    if replace {
        if let Ok(raw) = std::fs::metadata(destination) {
            use std::os::windows::fs::MetadataExt;
            if raw.file_attributes() & FILE_ATTRIBUTE_READONLY != 0 {
                flags |= FILE_RENAME_IGNORE_READONLY_ATTRIBUTE;
            }
        }
    }

    let mut info: FILE_RENAME_INFO = unsafe { std::mem::zeroed() };
    info.Anonymous.Flags = flags;
    info.RootDirectory = ptr::null_mut();
    info.FileNameLength = ((to.len().saturating_sub(1)) * 2) as u32;
    let name_offset = offset_of!(FILE_RENAME_INFO, FileName);
    let mut buffer = vec![0u8; name_offset + to.len() * 2];
    unsafe {
        ptr::copy_nonoverlapping(
            &info as *const FILE_RENAME_INFO as *const u8,
            buffer.as_mut_ptr(),
            size_of::<FILE_RENAME_INFO>(),
        );
        ptr::copy_nonoverlapping(
            to.as_ptr(),
            buffer.as_mut_ptr().add(name_offset) as *mut u16,
            to.len(),
        );
    }
    let ok = unsafe {
        SetFileInformationByHandle(
            handle,
            FileRenameInfoEx,
            buffer.as_ptr() as *const core::ffi::c_void,
            buffer.len() as u32,
        )
    };
    let error = unsafe { GetLastError() };
    let _ = unsafe { CloseHandle(handle) };
    if ok == 0 {
        return Err(SidecarError::new(format!(
            "无法安全替换 {}：系统错误 {error}",
            destination.display()
        )));
    }
    Ok(())
}

#[cfg(not(windows))]
fn commit_candidate(candidate: &Path, destination: &Path, replace: bool) -> Result<()> {
    if !replace && destination.exists() {
        return Err(SidecarError::new("目标文件已经存在，拒绝覆盖"));
    }
    std::fs::rename(candidate, destination)?;
    Ok(())
}

#[cfg(windows)]
fn copy_attributes(source: &Path, destination: &Path) -> Result<()> {
    use windows_sys::Win32::Storage::FileSystem::{
        GetFileAttributesW, SetFileAttributesW, FILE_ATTRIBUTE_ARCHIVE, FILE_ATTRIBUTE_HIDDEN,
        FILE_ATTRIBUTE_READONLY, FILE_ATTRIBUTE_SYSTEM,
    };
    const INVALID: u32 = u32::MAX;
    let source_wide = wide_path(source);
    let attributes = unsafe { GetFileAttributesW(source_wide.as_ptr()) };
    if attributes == INVALID {
        return Ok(());
    }
    let preserved = attributes
        & (FILE_ATTRIBUTE_READONLY
            | FILE_ATTRIBUTE_HIDDEN
            | FILE_ATTRIBUTE_SYSTEM
            | FILE_ATTRIBUTE_ARCHIVE);
    if preserved == 0 {
        return Ok(());
    }
    let destination_wide = wide_path(destination);
    if unsafe { SetFileAttributesW(destination_wide.as_ptr(), preserved) } == 0 {
        return Err(SidecarError::new(format!(
            "无法保留文件属性：{}",
            destination.display()
        )));
    }
    Ok(())
}

#[cfg(not(windows))]
fn copy_attributes(_source: &Path, _destination: &Path) -> Result<()> {
    Ok(())
}

/// Copy the extracted file to a sibling candidate, verify it, then commit the
/// directory entry with a same-volume rename. The old destination is never
/// opened for writing, so a failure before the commit leaves it untouched.
pub fn replace_file(source: &Path, destination: &Path, token: &AtomicBool) -> Result<()> {
    if let Some(parent) = destination.parent() {
        ensure_dir(parent)?;
    }
    let existing = match std::fs::symlink_metadata(destination) {
        Ok(meta) => Some(meta),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => None,
        Err(error) => {
            return Err(SidecarError::new(format!(
                "无法检查目标 {}：{error}",
                destination.display()
            )))
        }
    };
    if let Some(meta) = existing.as_ref() {
        if is_reparse_point(meta) {
            return Err(SidecarError::new(format!(
                "恢复目标包含链接或重解析点，已拒绝写入：{}",
                destination.display()
            )));
        }
        if meta.is_dir() {
            return Err(SidecarError::new(format!(
                "恢复目标是目录，不能用文件覆盖：{}",
                destination.display()
            )));
        }
    }

    let candidate = candidate_path(destination);
    let mut input = File::open(source)?;
    let mut output = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&candidate)
        .map_err(|error| {
            SidecarError::new(format!("无法创建替换候选 {}：{error}", candidate.display()))
        })?;
    let copy_result = copy_stream(&mut input, &mut output, token);
    drop(output);
    if let Err(error) = copy_result {
        let _ = std::fs::remove_file(&candidate);
        return Err(error);
    }

    let prepared = (|| -> Result<()> {
        let source_meta = std::fs::metadata(source)?;
        let source_size = source_meta.len();
        if std::fs::metadata(&candidate)?.len() != source_size {
            return Err(SidecarError::new("替换候选长度校验失败"));
        }
        if hash_file(&candidate)?.0 != hash_file(source)?.0 {
            return Err(SidecarError::new("替换候选 SHA-256 校验失败"));
        }
        let mtime = filetime::FileTime::from_last_modification_time(&source_meta);
        let atime = filetime::FileTime::from_last_access_time(&source_meta);
        let _ = filetime::set_file_times(&candidate, atime, mtime);
        if existing.is_some() {
            copy_attributes(destination, &candidate)?;
        }
        commit_candidate(&candidate, destination, existing.is_some())
    })();
    if prepared.is_err() {
        let _ = std::fs::remove_file(&candidate);
    }
    prepared
}

pub fn ensure_dir(path: &Path) -> Result<()> {
    std::fs::create_dir_all(path)
        .map_err(|error| SidecarError::new(format!("无法创建目录 {}：{error}", path.display())))
}

/// Reject reparse points in the destination path below the selected root.
/// The root itself may be a redirect chosen by the user.
pub fn ensure_no_reparse_chain(root: &Path, relative: &Path) -> Result<()> {
    let mut current = root.to_path_buf();
    for component in relative.components() {
        current.push(component.as_os_str());
        match std::fs::symlink_metadata(&current) {
            Ok(meta) => {
                if is_reparse_point(&meta) {
                    return Err(SidecarError::new(format!(
                        "恢复路径包含链接或重解析点，已拒绝写入：{}",
                        current.display()
                    )));
                }
                if !meta.is_dir() {
                    return Err(SidecarError::new(format!(
                        "恢复路径中的目录位置被文件占用：{}",
                        current.display()
                    )));
                }
            }
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
            Err(error) => {
                return Err(SidecarError::new(format!(
                    "无法检查恢复路径 {}：{error}",
                    current.display()
                )))
            }
        }
    }
    Ok(())
}

pub fn write_new(path: &Path, bytes: &[u8]) -> Result<()> {
    if let Some(parent) = path.parent() {
        ensure_dir(parent)?;
    }
    let mut file = OpenOptions::new().write(true).create_new(true).open(path)?;
    file.write_all(bytes)?;
    file.sync_all()?;
    Ok(())
}

pub fn canonical(path: &Path) -> Result<PathBuf> {
    std::fs::canonicalize(path)
        .map_err(|error| SidecarError::new(format!("无法解析路径 {}：{error}", path.display())))
}

#[cfg(windows)]
pub fn hide_console(command: &mut std::process::Command) {
    use std::os::windows::process::CommandExt;
    const CREATE_NO_WINDOW: u32 = 0x0800_0000;
    command.creation_flags(CREATE_NO_WINDOW);
}

#[cfg(not(windows))]
pub fn hide_console(_command: &mut std::process::Command) {}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!(
            "dsh-util-{name}-{}-{}",
            std::process::id(),
            CANDIDATE_COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn stable_copy_is_independent_of_later_source_writes() {
        let dir = temp_dir("stable");
        let source = dir.join("source.bin");
        let destination = dir.join("staged.bin");
        std::fs::write(&source, b"original-content").unwrap();
        stable_copy(&source, &destination, &AtomicBool::new(false)).unwrap();
        std::fs::write(&source, b"changed").unwrap();
        assert_eq!(std::fs::read(&destination).unwrap(), b"original-content");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn failed_copy_does_not_destroy_existing_destination() {
        let dir = temp_dir("replace");
        let source = dir.join("source.bin");
        let destination = dir.join("destination.bin");
        std::fs::write(&source, b"new-value").unwrap();
        std::fs::write(&destination, b"old-value").unwrap();
        let token = AtomicBool::new(true);
        assert!(replace_file(&source, &destination, &token).is_err());
        assert_eq!(std::fs::read(&destination).unwrap(), b"old-value");
        let _ = std::fs::remove_dir_all(&dir);
    }
}
