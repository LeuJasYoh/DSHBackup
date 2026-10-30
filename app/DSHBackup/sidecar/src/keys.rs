use crate::error::{Result, SidecarError};
use crate::util::{ensure_dir, write_new};
use age::secrecy::ExposeSecret;
use age::x25519;
use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::Mutex;

static KEY_LOCK: Mutex<()> = Mutex::new(());
const MAX_WRAPPED_BYTES: usize = 64 * 1024;

pub struct KeyPaths {
    pub dir: PathBuf,
    pub public: PathBuf,
    pub private: PathBuf,
}

impl KeyPaths {
    pub fn new(dir: &Path) -> Self {
        Self {
            dir: dir.to_path_buf(),
            public: dir.join("public.key"),
            private: dir.join("private.key"),
        }
    }
}

fn key_id(public_key: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(public_key.trim().as_bytes());
    hex::encode(hasher.finalize())
}

fn read_text(path: &Path) -> Result<String> {
    std::fs::read_to_string(path)
        .map_err(|error| SidecarError::new(format!("无法读取密钥文件 {}：{error}", path.display())))
}

fn parse_identity(secret: &str) -> Result<x25519::Identity> {
    secret
        .trim()
        .parse::<x25519::Identity>()
        .map_err(|error| SidecarError::new(format!("私钥格式无效：{error}")))
}

fn parse_recipient(public: &str) -> Result<x25519::Recipient> {
    public
        .trim()
        .parse::<x25519::Recipient>()
        .map_err(|error| SidecarError::new(format!("公钥格式无效：{error}")))
}

struct Pair {
    public: String,
    key_id: String,
}

/// Reads the private key and checks that the stored public key belongs to it.
/// A missing public key is reported; it is never replaced here.
fn inspect_pair(dir: &Path) -> Result<std::result::Result<Pair, String>> {
    let paths = KeyPaths::new(dir);
    if !paths.private.exists() {
        return Ok(Err("本地私钥不存在".to_string()));
    }
    let secret = read_text(&paths.private)?;
    let identity = parse_identity(&secret)?;
    let public = identity.to_public().to_string();
    if !paths.public.exists() {
        return Ok(Err("公钥文件缺失；可用原私钥重新生成公钥".to_string()));
    }
    let stored_public = read_text(&paths.public)?;
    if stored_public.trim() != public.trim() {
        return Ok(Err(
            "公钥与私钥不匹配。为避免已有备份失效，不会自动覆盖密钥。".to_string(),
        ));
    }
    Ok(Ok(Pair {
        key_id: key_id(&public),
        public,
    }))
}

pub fn ensure(dir: &Path, allow_create: bool) -> Result<Value> {
    let _guard = KEY_LOCK
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let paths = KeyPaths::new(dir);
    ensure_dir(&paths.dir)?;

    if paths.private.exists() {
        let secret = read_text(&paths.private)?;
        let identity = parse_identity(&secret)?;
        let public = identity.to_public().to_string();
        if !paths.public.exists() {
            write_new(&paths.public, format!("{}\n", public).as_bytes())?;
        }
        let stored_public = read_text(&paths.public)?;
        if stored_public.trim() != public.trim() {
            return Err(SidecarError::new(
                "公钥与私钥不匹配。为避免已有备份失效，请恢复正确的公钥文件。",
            ));
        }
        return Ok(json!({
            "exists": true,
            "generated": false,
            "canEncrypt": true,
            "publicKey": public,
            "keyId": key_id(&public),
            "privatePath": paths.private.to_string_lossy(),
        }));
    }

    if paths.public.exists() {
        return Err(SidecarError::new(
            "检测到公钥但找不到私钥。为避免已有加密备份永久失效，不会自动生成新密钥；请从备份中恢复 private.key。",
        ));
    }

    if !allow_create {
        return Err(SidecarError::new(
            "本地密钥尚未生成；只有确认没有旧加密备份后才允许创建新密钥。",
        ));
    }

    let identity = x25519::Identity::generate();
    let secret = identity.to_string();
    let public = identity.to_public().to_string();
    write_new(
        &paths.private,
        format!("{}\n", secret.expose_secret()).as_bytes(),
    )?;
    write_new(&paths.public, format!("{}\n", public).as_bytes())?;
    Ok(json!({
        "exists": true,
        "generated": true,
        "canEncrypt": true,
        "publicKey": public,
        "keyId": key_id(&public),
        "privatePath": paths.private.to_string_lossy(),
    }))
}

pub fn info(dir: &Path) -> Result<Value> {
    let paths = KeyPaths::new(dir);
    if !paths.private.exists() && !paths.public.exists() {
        return Ok(json!({
            "exists": false,
            "canEncrypt": false,
            "keyId": "",
            "publicKey": "",
            "privatePath": paths.private.to_string_lossy(),
        }));
    }
    if !paths.private.exists() {
        return Ok(json!({
            "exists": false,
            "canEncrypt": false,
            "keyId": "",
            "publicKey": std::fs::read_to_string(&paths.public).unwrap_or_default(),
            "privatePath": paths.private.to_string_lossy(),
            "encryptionError": "公钥存在但私钥缺失",
            "error": "公钥存在但私钥缺失",
        }));
    }
    let secret = read_text(&paths.private)?;
    let identity = parse_identity(&secret)?;
    let public = identity.to_public().to_string();
    let mut value = json!({
        "exists": true,
        "canEncrypt": false,
        "keyId": key_id(&public),
        "publicKey": public,
        "privatePath": paths.private.to_string_lossy(),
    });
    match inspect_pair(dir) {
        Ok(Ok(_)) => {
            value["canEncrypt"] = json!(true);
        }
        Ok(Err(message)) => {
            value["encryptionError"] = json!(message);
        }
        Err(error) => {
            value["encryptionError"] = json!(error.message);
        }
    }
    Ok(value)
}

pub fn wrap_password(dir: &Path, password: &str) -> Result<Value> {
    if password.is_empty() {
        return Err(SidecarError::new("归档密码不能为空"));
    }
    let pair = match inspect_pair(dir)? {
        Ok(value) => value,
        Err(message) => return Err(SidecarError::new(message)),
    };
    let recipient = parse_recipient(&pair.public)?;
    let encryptor =
        age::Encryptor::with_recipients(std::iter::once(&recipient as &dyn age::Recipient))
            .map_err(|error| SidecarError::new(format!("无法初始化加密器：{error}")))?;
    let mut encrypted = Vec::new();
    {
        let mut writer = encryptor
            .wrap_output(&mut encrypted)
            .map_err(|error| SidecarError::new(format!("封装密码失败：{error}")))?;
        writer.write_all(password.as_bytes())?;
        writer
            .finish()
            .map_err(|error| SidecarError::new(format!("封装密码失败：{error}")))?;
    }
    Ok(json!({
        "wrapped": BASE64.encode(encrypted),
        "keyId": pair.key_id,
    }))
}

pub fn unwrap_password(dir: &Path, wrapped: &str) -> Result<Value> {
    if wrapped.len() > MAX_WRAPPED_BYTES {
        return Err(SidecarError::new("封装的归档密码过大"));
    }
    let paths = KeyPaths::new(dir);
    if !paths.private.exists() {
        return Err(SidecarError::new(
            "本地私钥不存在，无法解密该归档。请把备份时的 data\\keys 目录复制到本机。",
        ));
    }
    let secret = read_text(&paths.private)?;
    let identity = parse_identity(&secret)?;
    let bytes = BASE64
        .decode(wrapped.trim())
        .map_err(|error| SidecarError::new(format!("封装的密码格式无效：{error}")))?;
    let decryptor = age::Decryptor::new(&bytes[..])
        .map_err(|error| SidecarError::new(format!("无法解析加密数据：{error}")))?;
    let reader = decryptor
        .decrypt(std::iter::once(&identity as &dyn age::Identity))
        .map_err(|error| SidecarError::new(format!("私钥无法解开归档密码：{error}")))?;
    let mut password = String::new();
    reader.take(2048).read_to_string(&mut password)?;
    if password.is_empty() {
        return Err(SidecarError::new("解出的归档密码为空"));
    }
    Ok(json!({ "password": password }))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};

    static TEST_COUNTER: AtomicU64 = AtomicU64::new(1);

    fn temp_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!(
            "dsh-key-{name}-{}-{}",
            std::process::id(),
            TEST_COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn round_trip_password() {
        let dir = temp_dir("roundtrip");
        let generated = ensure(&dir, true).unwrap();
        assert_eq!(generated["generated"], true);
        let public = generated["publicKey"].as_str().unwrap().to_string();
        let wrapped = wrap_password(&dir, "correct horse battery staple").unwrap();
        let unwrapped = unwrap_password(&dir, wrapped["wrapped"].as_str().unwrap()).unwrap();
        assert_eq!(unwrapped["password"], "correct horse battery staple");
        let info = info(&dir).unwrap();
        assert_eq!(info["publicKey"], public);
        assert_eq!(info["canEncrypt"], true);
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn create_requires_explicit_permission() {
        let dir = temp_dir("permission");
        assert!(ensure(&dir, false).is_err());
        assert!(!dir.join("private.key").exists());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn mismatched_public_key_blocks_encryption_but_keeps_private_usable() {
        let dir = temp_dir("mismatch");
        ensure(&dir, true).unwrap();
        let other = temp_dir("other");
        let other_key = ensure(&other, true).unwrap();
        std::fs::write(
            dir.join("public.key"),
            format!("{}\n", other_key["publicKey"].as_str().unwrap()),
        )
        .unwrap();
        assert!(wrap_password(&dir, "secret").is_err());
        let info = info(&dir).unwrap();
        assert_eq!(info["exists"], true);
        assert_eq!(info["canEncrypt"], false);
        let _ = std::fs::remove_dir_all(&dir);
        let _ = std::fs::remove_dir_all(&other);
    }

    #[test]
    fn missing_public_key_is_reported_and_repairable() {
        let dir = temp_dir("public-missing");
        ensure(&dir, true).unwrap();
        let key_id = info(&dir).unwrap()["keyId"].as_str().unwrap().to_string();
        std::fs::remove_file(dir.join("public.key")).unwrap();
        let before = info(&dir).unwrap();
        assert_eq!(before["canEncrypt"], false);
        let repaired = ensure(&dir, false).unwrap();
        assert_eq!(repaired["keyId"].as_str().unwrap(), key_id);
        assert!(dir.join("public.key").exists());
        let _ = std::fs::remove_dir_all(&dir);
    }
}
