//! Rejects runner API requests that originate inside the runner's own pod.
//!
//! The runner API (`/state`, `/threads/{id}/turn`, `/threads/{id}/interrupt`)
//! is meant for the Speakeasy server only, which always dials in from another
//! host. Code the assistant runs through `bun_run` shares the pod's network
//! namespace, so without this guard it could reach the API on loopback or on
//! the pod's own address and list or interrupt other threads on the same
//! runtime.

use std::net::{IpAddr, SocketAddr};

use axum::extract::connect_info::Connected;
use axum::extract::{ConnectInfo, Request};
use axum::http::StatusCode;
use axum::middleware::Next;
use axum::response::{IntoResponse, Response};
use axum::serve::IncomingStream;
use tokio::net::TcpListener;

/// Both ends of an accepted connection.
#[derive(Clone, Copy, Debug)]
pub struct PeerAddrs {
    /// Address the connection was accepted on. `None` only if the socket
    /// could not report it, which the guard treats as same-host.
    pub local: Option<SocketAddr>,
    pub remote: SocketAddr,
}

impl Connected<IncomingStream<'_, TcpListener>> for PeerAddrs {
    fn connect_info(stream: IncomingStream<'_, TcpListener>) -> Self {
        Self {
            local: stream.io().local_addr().ok(),
            remote: *stream.remote_addr(),
        }
    }
}

/// Whether the connection came from this host. A connection to one of the
/// host's own addresses is sourced from that same address, so a remote
/// address equal to the local one means a local caller.
pub fn is_same_host(peer: &PeerAddrs) -> bool {
    let remote = canonical(peer.remote.ip());
    if remote.is_loopback() || remote.is_unspecified() {
        return true;
    }
    match peer.local {
        Some(local) => canonical(local.ip()) == remote,
        None => true,
    }
}

fn canonical(ip: IpAddr) -> IpAddr {
    match ip {
        IpAddr::V6(v6) => v6.to_ipv4_mapped().map_or(IpAddr::V6(v6), IpAddr::V4),
        v4 @ IpAddr::V4(_) => v4,
    }
}

pub async fn reject_same_host(
    ConnectInfo(peer): ConnectInfo<PeerAddrs>,
    request: Request,
    next: Next,
) -> Response {
    if is_same_host(&peer) {
        tracing::warn!(
            remote = %peer.remote,
            path = %request.uri().path(),
            "rejected runner API request from inside the runtime"
        );
        return (StatusCode::FORBIDDEN, "forbidden").into_response();
    }
    next.run(request).await
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;

    fn peer(local: &str, remote: &str) -> PeerAddrs {
        PeerAddrs {
            local: Some(local.parse().unwrap()),
            remote: remote.parse().unwrap(),
        }
    }

    #[test]
    fn loopback_callers_are_same_host() {
        assert!(is_same_host(&peer("127.0.0.1:8081", "127.0.0.1:40000")));
        assert!(is_same_host(&peer("[::1]:8081", "[::1]:40000")));
        assert!(is_same_host(&peer("[::]:8081", "[::ffff:127.0.0.1]:40000")));
    }

    #[test]
    fn callers_using_the_pods_own_address_are_same_host() {
        assert!(is_same_host(&peer("10.4.0.7:8081", "10.4.0.7:40000")));
        assert!(is_same_host(&peer(
            "[::ffff:10.4.0.7]:8081",
            "10.4.0.7:40000"
        )));
    }

    #[test]
    fn unknown_local_address_fails_closed() {
        let p = PeerAddrs {
            local: None,
            remote: "10.8.0.3:40000".parse().unwrap(),
        };
        assert!(is_same_host(&p));
    }

    #[test]
    fn remote_callers_are_allowed() {
        assert!(!is_same_host(&peer("10.4.0.7:8081", "10.8.0.3:40000")));
        // Local Docker: the host reaches the container through the bridge gateway.
        assert!(!is_same_host(&peer("172.17.0.2:8081", "172.17.0.1:40000")));
    }
}
