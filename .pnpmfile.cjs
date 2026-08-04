const unusedNuqsAdapterPeers = ["next", "react-router-dom"];

module.exports = {
  hooks: {
    readPackage(pkg) {
      if (pkg.name !== "nuqs") {
        return pkg;
      }

      // The dashboard uses the React Router v8 adapter. Keep unrelated optional
      // adapter peers out of the workspace graph so their trees are not installed.
      for (const peer of unusedNuqsAdapterPeers) {
        delete pkg.peerDependencies?.[peer];
        delete pkg.peerDependenciesMeta?.[peer];
      }

      return pkg;
    },
  },
};
