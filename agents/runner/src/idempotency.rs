use std::collections::{HashSet, VecDeque};

pub const HEADER: &str = "x-idempotency-key";
const CAPACITY: usize = 1024;

// Bounded FIFO set of idempotency keys already processed by /turn. Dedup is
// best-effort memory-only: when the Go side redelivers the same event (activity
// retry, coordinator re-signal, reaper requeue), we skip re-running the turn
// and reply with a canned ack. Capacity bounds memory; oldest keys age out
// once the cap is hit.
#[derive(Default)]
pub struct IdempotencyCache {
    set: HashSet<String>,
    order: VecDeque<String>,
}

impl IdempotencyCache {
    pub fn contains(&self, key: &str) -> bool {
        self.set.contains(key)
    }

    pub fn insert(&mut self, key: String) {
        if self.set.contains(&key) {
            return;
        }
        if self.order.len() >= CAPACITY
            && let Some(evicted) = self.order.pop_front()
        {
            self.set.remove(&evicted);
        }
        self.set.insert(key.clone());
        self.order.push_back(key);
    }
}
