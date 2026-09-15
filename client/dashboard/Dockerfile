FROM nginxinc/nginx-unprivileged:alpine

# Copy the build output to nginx
COPY dist /usr/share/nginx/html
COPY --chown=nginx:nginx dist/index.html /usr/share/nginx/html/index.html

# nginx.conf is an envsubst template: the image entrypoint renders it to
# /etc/nginx/conf.d/default.conf at startup, substituting ${GRAM_CDN_URL}.
# A later entrypoint script substitutes the GRAM_* values below in index.html.
# The nginx entrypoint runs /docker-entrypoint.d/* in `sort -V` order. This
# script carries no numeric prefix, so it sorts after every numbered script
# the base image ships — which is what we want: it must follow
# 20-envsubst-on-templates.sh.
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY --chmod=755 preview-runtime-values.sh /docker-entrypoint.d/

# Defaults: no CDN rewrite, no admin link, no deployment provenance. All must
# always be defined — envsubst only substitutes variables present in the
# environment, and unsubstituted values would be parsed by nginx as undefined
# nginx variables at startup, or left as literal ${...} text in index.html.
ENV GRAM_CDN_URL=""
ENV GRAM_ADMIN_SERVER_URL=""

# Deployment provenance, set per environment by the deployment (see the Helm
# values in gram-infra). The Gutternote widget in index.html needs both
# GRAM_IS_PREVIEW="true" and a non-empty GRAM_GUTTERNOTE_KEY, so these empty
# defaults keep it off everywhere it is not deliberately configured.
# GRAM_GUTTERNOTE_KEY is a public client-side key served in the HTML, not a
# secret; it lives in the environment so it can be rotated without a rebuild.
ENV GRAM_BRANCH=""
ENV GRAM_COMMIT_SHA=""
ENV GRAM_PR_NUMBER=""
ENV GRAM_IS_PREVIEW=""
ENV GRAM_GUTTERNOTE_KEY=""

EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]