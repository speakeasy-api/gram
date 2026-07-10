use std::path::{Path, PathBuf};
use std::time::Duration;

use crate::gram_client::GramBootstrapClient;
use crate::http_layer::TokenRegistry;

#[derive(Debug)]
pub struct WorkspaceMonitorConfig {
    pub path: PathBuf,
    pub threshold_percent: u8,
    pub check_interval: Duration,
}

pub fn spawn_monitor(
    config: WorkspaceMonitorConfig,
    gram_client: GramBootstrapClient,
    tokens: TokenRegistry,
) {
    tokio::spawn(async move {
        monitor(config, gram_client, tokens).await;
    });
}

async fn monitor(
    config: WorkspaceMonitorConfig,
    gram_client: GramBootstrapClient,
    tokens: TokenRegistry,
) {
    let mut interval = tokio::time::interval(config.check_interval);
    interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    let mut awaiting_growth_from = None;

    loop {
        interval.tick().await;
        let path = config.path.clone();
        let stats = tokio::task::spawn_blocking(move || disk_space(&path)).await;
        let (total, available) = match stats {
            Ok(Ok(stats)) => stats,
            Ok(Err(error)) => {
                tracing::warn!(path = %config.path.display(), error = %error, "read assistant workspace usage failed");
                continue;
            }
            Err(error) => {
                tracing::warn!(error = %error, "assistant workspace usage task failed");
                continue;
            }
        };

        if let Some(previous_total) = awaiting_growth_from {
            if total <= previous_total {
                continue;
            }
            awaiting_growth_from = None;
        }
        if !usage_at_or_above_threshold(total, available, config.threshold_percent) {
            continue;
        }

        match gram_client.grow_workspace(&tokens).await {
            Ok(result) if result.expanded => {
                awaiting_growth_from = Some(total);
                tracing::info!(
                    current_bytes = result.current_bytes,
                    requested_bytes = result.requested_bytes,
                    "assistant workspace growth requested"
                );
            }
            Ok(result) => {
                tracing::info!(
                    current_bytes = result.current_bytes,
                    requested_bytes = result.requested_bytes,
                    "assistant workspace reached its configured maximum"
                );
                return;
            }
            Err(error) => {
                tracing::warn!(error = %error, "assistant workspace growth request failed");
            }
        }
    }
}

fn disk_space(path: &Path) -> Result<(u64, u64), std::io::Error> {
    Ok((fs2::total_space(path)?, fs2::available_space(path)?))
}

fn usage_at_or_above_threshold(total: u64, available: u64, threshold_percent: u8) -> bool {
    if total == 0 {
        return false;
    }
    let used = total.saturating_sub(available);
    u128::from(used) * 100 >= u128::from(total) * u128::from(threshold_percent)
}

#[cfg(test)]
mod tests {
    use super::usage_at_or_above_threshold;

    #[test]
    fn grows_at_threshold() {
        assert!(usage_at_or_above_threshold(100, 20, 80));
    }

    #[test]
    fn waits_below_threshold() {
        assert!(!usage_at_or_above_threshold(100, 21, 80));
    }

    #[test]
    fn ignores_zero_sized_filesystem() {
        assert!(!usage_at_or_above_threshold(0, 0, 80));
    }
}
