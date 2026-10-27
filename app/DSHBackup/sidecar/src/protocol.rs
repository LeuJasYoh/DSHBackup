use crate::error::{Result, SidecarError};
use serde::Deserialize;
use serde_json::{json, Value};
use std::io::{BufWriter, Stdout, Write};
use std::sync::{Arc, Mutex};

pub type SharedWriter = Arc<Mutex<BufWriter<Stdout>>>;

#[derive(Debug, Deserialize)]
pub struct Request {
    #[serde(default)]
    pub id: Value,
    pub method: String,
    #[serde(default)]
    pub params: Value,
}

#[derive(Clone)]
pub struct Emitter {
    writer: SharedWriter,
    pub task_id: Option<String>,
}

impl Emitter {
    pub fn new(writer: SharedWriter, task_id: Option<String>) -> Self {
        Self { writer, task_id }
    }

    fn write(&self, value: Value) {
        let mut guard = match self.writer.lock() {
            Ok(guard) => guard,
            Err(poisoned) => poisoned.into_inner(),
        };
        let _ = serde_json::to_writer(&mut *guard, &value);
        let _ = guard.write_all(b"\n");
        let _ = guard.flush();
    }

    pub fn respond(&self, id: &Value, result: Value) {
        self.write(json!({"jsonrpc": "2.0", "id": id, "result": result}));
    }

    pub fn fail(&self, id: &Value, error: &SidecarError) {
        let mut payload = json!({
            "jsonrpc": "2.0",
            "id": id,
            "error": {
                "code": error.code,
                "message": error.message,
            }
        });
        if let Some(data) = &error.data {
            payload["error"]["data"] = data.clone();
        }
        self.write(payload);
    }

    pub fn progress(
        &self,
        phase: &str,
        percent: f64,
        message: &str,
        bytes_done: Option<u64>,
        bytes_total: Option<u64>,
    ) {
        let Some(task_id) = &self.task_id else {
            return;
        };
        let percent = percent.clamp(0.0, 100.0);
        let mut params = json!({
            "taskId": task_id,
            "phase": phase,
            "percent": percent,
            "message": message,
        });
        if let Some(done) = bytes_done {
            params["bytesDone"] = json!(done);
        }
        if let Some(total) = bytes_total {
            params["bytesTotal"] = json!(total);
        }
        self.write(json!({"jsonrpc": "2.0", "method": "progress", "params": params}));
    }
}

pub fn parse_request(line: &str) -> Result<Request> {
    serde_json::from_str(line).map_err(|error| SidecarError::new(format!("无法解析请求：{error}")))
}

pub fn make_writer() -> SharedWriter {
    Arc::new(Mutex::new(BufWriter::new(std::io::stdout())))
}
