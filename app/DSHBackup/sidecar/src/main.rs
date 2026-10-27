mod error;
mod keys;
mod pathsafe;
mod protocol;
mod sevenzip;
mod staging;
mod util;

use crate::error::{Result, SidecarError};
use crate::protocol::{Emitter, Request, SharedWriter};
use serde::Deserialize;
use serde_json::{json, Value};
use std::collections::HashMap;
use std::io::BufRead;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

type CancelMap = Arc<Mutex<HashMap<String, Arc<AtomicBool>>>>;

fn main() {
    let writer = protocol::make_writer();
    let cancellations: CancelMap = Arc::new(Mutex::new(HashMap::new()));
    eprintln!(
        "dsh-backup-core {} (protocol 1) started",
        env!("CARGO_PKG_VERSION")
    );

    let stdin = std::io::stdin();
    for line in stdin.lock().lines() {
        let line = match line {
            Ok(value) => value,
            Err(error) => {
                eprintln!("stdin error: {error}");
                break;
            }
        };
        if line.trim().is_empty() {
            continue;
        }
        let request = match protocol::parse_request(&line) {
            Ok(value) => value,
            Err(error) => {
                let emitter = Emitter::new(writer.clone(), None);
                emitter.fail(&Value::Null, &error);
                continue;
            }
        };
        if request.method == "cancel" {
            let task_id = request
                .params
                .get("taskId")
                .and_then(Value::as_str)
                .unwrap_or_default()
                .to_string();
            let cancelled = if let Ok(map) = cancellations.lock() {
                if let Some(token) = map.get(&task_id) {
                    token.store(true, Ordering::SeqCst);
                    true
                } else {
                    false
                }
            } else {
                false
            };
            let emitter = Emitter::new(writer.clone(), None);
            emitter.respond(&request.id, json!({ "cancelled": cancelled }));
            continue;
        }

        let writer_clone = writer.clone();
        let cancellations_clone = cancellations.clone();
        std::thread::spawn(move || {
            handle(request, writer_clone, cancellations_clone);
        });
    }
}

fn handle(request: Request, writer: SharedWriter, cancellations: CancelMap) {
    let task_id = request
        .params
        .get("taskId")
        .and_then(Value::as_str)
        .map(str::to_string);
    let emitter = Emitter::new(writer, task_id.clone());
    let token = Arc::new(AtomicBool::new(false));
    if let Some(id) = task_id.as_ref() {
        if let Ok(mut map) = cancellations.lock() {
            map.insert(id.clone(), token.clone());
        }
    }

    let result = dispatch_inner(&request, &token, &emitter);

    if let Some(id) = task_id {
        if let Ok(mut map) = cancellations.lock() {
            map.remove(&id);
        }
    }
    match result {
        Ok(value) => emitter.respond(&request.id, value),
        Err(error) => emitter.fail(&request.id, &error),
    }
}

fn dispatch_inner(request: &Request, token: &AtomicBool, emitter: &Emitter) -> Result<Value> {
    match request.method.as_str() {
        "ping" => Ok(json!({
            "version": env!("CARGO_PKG_VERSION"),
            "protocol": 1,
        })),
        "check_7z" => {
            let params: sevenzip::CheckRequest = serde_json::from_value(request.params.clone())?;
            sevenzip::check(params)
        }
        "stage" => {
            let params: staging::StageRequest = serde_json::from_value(request.params.clone())?;
            staging::stage(params, token, emitter)
        }
        "backup" => {
            let params: sevenzip::BackupRequest =
                serde_json::from_value(request.params.clone())?;
            sevenzip::backup(params, token, emitter)
        }
        "list_archive" => {
            let params: sevenzip::ListRequest =
                serde_json::from_value(request.params.clone())?;
            sevenzip::list(params)
        }
        "validate_archive" => {
            let params: sevenzip::ListRequest =
                serde_json::from_value(request.params.clone())?;
            sevenzip::validate_archive(
                &params.seven_zip,
                &params.archive,
                params.password.as_deref(),
            )
        }
        "test_archive" => {
            let params: sevenzip::TestRequest =
                serde_json::from_value(request.params.clone())?;
            sevenzip::test(params, token, emitter)
        }
        "extract" => {
            let params: sevenzip::ExtractRequest =
                serde_json::from_value(request.params.clone())?;
            sevenzip::extract(params, token, emitter)
        }
        "hash_file" => {
            let params: HashRequest = serde_json::from_value(request.params.clone())?;
            let (sha256, size) = util::hash_file(&params.path)?;
            Ok(json!({ "sha256": sha256, "size": size }))
        }
        "keys_ensure" => {
            let params: KeyDirRequest = serde_json::from_value(request.params.clone())?;
            keys::ensure(&params.dir)
        }
        "keys_info" => {
            let params: KeyDirRequest = serde_json::from_value(request.params.clone())?;
            keys::info(&params.dir)
        }
        "wrap_password" => {
            let params: WrapRequest = serde_json::from_value(request.params.clone())?;
            keys::wrap_password(&params.dir, &params.password)
        }
        "unwrap_password" => {
            let params: UnwrapRequest = serde_json::from_value(request.params.clone())?;
            keys::unwrap_password(&params.dir, &params.wrapped)
        }
        "install_staging" => {
            let params: InstallRequest = serde_json::from_value(request.params.clone())?;
            let targets = params
                .targets
                .into_iter()
                .map(|target| (target.name, target.path))
                .collect::<Vec<_>>();
            staging::install_staging(&params.staging_dir, &targets, token, emitter)
        }
        "sleep" => {
            let params: SleepRequest = serde_json::from_value(request.params.clone())?;
            let end = std::time::Instant::now() + Duration::from_millis(params.millis);
            while std::time::Instant::now() < end {
                if token.load(Ordering::SeqCst) {
                    return Err(SidecarError::cancelled());
                }
                std::thread::sleep(Duration::from_millis(50));
            }
            Ok(json!({ "slept": params.millis }))
        }
        other => Err(SidecarError::new(format!("未知方法：{other}"))),
    }
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct HashRequest {
    path: PathBuf,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct KeyDirRequest {
    dir: PathBuf,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct WrapRequest {
    dir: PathBuf,
    password: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct UnwrapRequest {
    dir: PathBuf,
    wrapped: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct InstallRequest {
    staging_dir: PathBuf,
    targets: Vec<InstallTarget>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct InstallTarget {
    name: String,
    path: PathBuf,
}

#[derive(Deserialize)]
struct SleepRequest {
    millis: u64,
}
