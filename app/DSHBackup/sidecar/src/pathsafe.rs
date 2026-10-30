use crate::error::{Result, SidecarError};
use std::path::Path;

const ALLOWED_ROOTS: [&str; 2] = ["dsh", "desktop-state"];
const RESERVED_NAMES: [&str; 30] = [
    "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8",
    "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "CONIN$",
    "CONOUT$", "COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³",
];

pub fn validate_archive_path(raw: &str) -> Result<String> {
    if raw.is_empty() {
        return Err(SidecarError::new("归档中包含空路径"));
    }
    if raw != raw.trim() {
        return Err(SidecarError::new(format!(
            "归档路径以空白开头或结尾：{raw}"
        )));
    }
    if raw.contains('\0') {
        return Err(SidecarError::new("归档路径包含非法字符"));
    }
    let normalized = raw.replace('\\', "/");
    let without_trailing = normalized.trim_end_matches('/');
    if without_trailing.is_empty() {
        return Err(SidecarError::new("归档路径不能是根目录"));
    }
    if without_trailing.starts_with('/') {
        return Err(SidecarError::new(format!("归档中包含绝对路径：{raw}")));
    }
    let bytes = without_trailing.as_bytes();
    if bytes.len() >= 2 && bytes[1] == b':' {
        return Err(SidecarError::new(format!("归档中包含盘符路径：{raw}")));
    }
    if without_trailing.encode_utf16().count() > 32767 {
        return Err(SidecarError::new("归档路径总长度超出 Windows 限制"));
    }
    let parts: Vec<&str> = without_trailing.split('/').collect();
    if parts.is_empty() {
        return Err(SidecarError::new("归档路径为空"));
    }
    let root = parts[0].to_ascii_lowercase();
    if !ALLOWED_ROOTS.contains(&root.as_str()) {
        return Err(SidecarError::new(format!(
            "归档根目录不在允许范围内：{raw}"
        )));
    }
    for part in &parts {
        validate_component(part, raw)?;
    }
    Ok(parts.join("/"))
}

fn validate_component(part: &str, raw: &str) -> Result<()> {
    if part.is_empty() {
        return Err(SidecarError::new(format!("归档路径包含空片段：{raw}")));
    }
    if part == "." || part == ".." {
        return Err(SidecarError::new(format!("归档路径包含路径穿越：{raw}")));
    }
    if part.contains(':') {
        return Err(SidecarError::new(format!(
            "归档路径包含数据流或盘符：{raw}"
        )));
    }
    if part
        .chars()
        .any(|ch| matches!(ch, '<' | '>' | '"' | '|' | '?' | '*'))
    {
        return Err(SidecarError::new(format!(
            "归档路径包含 Windows 非法字符：{raw}"
        )));
    }
    if part.chars().any(|ch| (ch as u32) < 0x20) {
        return Err(SidecarError::new(format!("归档路径包含控制字符：{raw}")));
    }
    if part.ends_with(' ') || part.ends_with('.') {
        return Err(SidecarError::new(format!("归档路径以空格或点结尾：{raw}")));
    }
    if part.encode_utf16().count() > 255 {
        return Err(SidecarError::new(format!("归档路径片段过长：{raw}")));
    }
    let stem = part
        .split('.')
        .next()
        .unwrap_or(part)
        .trim_end_matches(' ')
        .to_ascii_uppercase();
    if RESERVED_NAMES.contains(&stem.as_str()) {
        return Err(SidecarError::new(format!(
            "归档路径使用了 Windows 保留名：{raw}"
        )));
    }
    Ok(())
}

pub fn ensure_inside(base: &Path, candidate: &Path) -> Result<()> {
    let canonical_base = crate::util::canonical(base)?;
    let canonical_candidate = crate::util::canonical(candidate)?;
    if !canonical_candidate.starts_with(&canonical_base) {
        return Err(SidecarError::new(format!(
            "路径逃逸：{}",
            candidate.display()
        )));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accepts_normal_paths() {
        assert_eq!(
            validate_archive_path("dsh/sessions/a.json").unwrap(),
            "dsh/sessions/a.json"
        );
        assert_eq!(
            validate_archive_path("desktop-state\\Local Storage\\leveldb\\a.ldb").unwrap(),
            "desktop-state/Local Storage/leveldb/a.ldb"
        );
        assert!(validate_archive_path("dsh").is_ok());
    }

    #[test]
    fn rejects_escapes() {
        for path in [
            "../etc/passwd",
            "dsh/../../etc/passwd",
            "C:\\Windows\\win.ini",
            "\\\\server\\share\\file",
            "/etc/passwd",
            "other/file.txt",
            "dsh/..\\evil",
            "dsh/file.txt:stream",
            "dsh/CON",
            "dsh/trailing.",
            "dsh/trailing ",
            "dsh//double",
        ] {
            assert!(
                validate_archive_path(path).is_err(),
                "{path} should be rejected"
            );
        }
    }
}
