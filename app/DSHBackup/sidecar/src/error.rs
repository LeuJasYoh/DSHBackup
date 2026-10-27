use serde_json::Value;

#[derive(Debug)]
pub struct SidecarError {
    pub code: i64,
    pub message: String,
    pub data: Option<Value>,
}

impl SidecarError {
    pub fn new(message: impl Into<String>) -> Self {
        Self {
            code: -32000,
            message: message.into(),
            data: None,
        }
    }

    pub fn with_data(message: impl Into<String>, data: Value) -> Self {
        Self {
            code: -32000,
            message: message.into(),
            data: Some(data),
        }
    }

    pub fn cancelled() -> Self {
        Self {
            code: -32800,
            message: "任务已取消".to_string(),
            data: None,
        }
    }
}

impl std::fmt::Display for SidecarError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for SidecarError {}

impl From<std::io::Error> for SidecarError {
    fn from(value: std::io::Error) -> Self {
        Self::new(format!("文件操作失败：{value}"))
    }
}

impl From<serde_json::Error> for SidecarError {
    fn from(value: serde_json::Error) -> Self {
        Self::new(format!("数据格式错误：{value}"))
    }
}

pub type Result<T> = std::result::Result<T, SidecarError>;
