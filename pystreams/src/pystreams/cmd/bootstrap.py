"""Pristine entrypoint that stands up the scan pool's forkserver before the heavy
scientific stack is imported, then hands off to the real ``multi`` CLI.

Why this exists
---------------
:class:`~pystreams.risk.scanner.ProcessPoolScanner` runs Presidio in a
``forkserver``-backed process pool. The forkserver *process* is itself created by
``fork()``+``exec()``-ing whichever process first triggers it. By the time the
``multi`` CLI gets there it has imported numpy/spaCy (via the scanner) and the
Pub/Sub gRPC clients — on macOS that loads the Objective-C runtime (numpy's BLAS
backend is Accelerate, a Foundation framework) and spawns helper threads. macOS
then aborts the forked child if another thread was mid Objective-C ``+initialize``
at fork time::

    objc[...]: +[NSNumber initialize] may have been in progress in another thread
    when fork() was called. ... Crashing instead.

The fix is ordering, not a flag. The *only* fork that inherits the laden parent is
the forkserver bootstrap; every scan worker is forked from the forkserver, never
from the CLI process. So we bootstrap the forkserver here, while this process is
still pristine — no numpy, no gRPC, no Objective-C frameworks loaded — by starting
a throwaway process against the forkserver context. After that the forkserver
singleton is up; the pool created later in ``multi`` reuses it instead of
bootstrapping from the (by then numpy-laden) CLI process.

We deliberately do *not* ``set_forkserver_preload`` the scanner module: the
forkserver only ever ``fork()``s, and each worker imports numpy/Presidio in the
forked child (post-fork), so the forkserver itself stays single-threaded and every
worker fork is safe. Preloading would pull numpy into the forkserver and risk it
gaining background threads, reintroducing the very hazard on each worker fork.

Keep this module's top-level imports trivial: ``pystreams.cmd.multi`` (which drags
in numpy via the scanner) must not be imported until *after* the forkserver is up,
so the import lives inside :func:`main`.
"""

from __future__ import annotations

import multiprocessing
import os


def _noop() -> None:
    """Throwaway target whose only job is to force the forkserver to bootstrap.

    Defined at module scope so it is picklable by reference; the forked child
    imports this module (which stays pristine — just ``multiprocessing``), runs
    nothing, and exits.
    """


def main() -> None:
    # Cap the native BLAS/OpenMP thread pools before numpy or spaCy load in any
    # process. Each scan-pool worker is sized to own ~one core; left uncapped,
    # a worker's BLAS backend spawns a thread per host core and the pool
    # oversubscribes the pod's CPU request. Set here — before the forkserver
    # bootstraps — so the forkserver, every worker forked from it, and this
    # process itself all inherit the caps. ``setdefault`` lets an explicit
    # override in the container environment win.
    for var in (
        "OMP_NUM_THREADS",
        "OPENBLAS_NUM_THREADS",
        "MKL_NUM_THREADS",
        "BLIS_NUM_THREADS",
        "VECLIB_MAXIMUM_THREADS",
    ):
        os.environ.setdefault(var, "1")

    # Bootstrap the forkserver while this process is still pristine. Starting any
    # process against the forkserver context triggers its one-time fork()+exec()
    # bootstrap; doing it now means that bootstrap forks a clean parent rather than
    # the numpy/gRPC/Objective-C-laden CLI process (which macOS would abort).
    warm = multiprocessing.get_context("forkserver").Process(target=_noop)
    warm.start()
    warm.join()

    # Safe now: the forkserver singleton is running. Importing the CLI (and the
    # scientific stack it pulls in) no longer affects pool worker spawning, which
    # forks from the forkserver, not from this process.
    from pystreams.cmd.multi import cli

    cli()


if __name__ == "__main__":
    main()
