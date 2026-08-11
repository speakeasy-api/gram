FROM nginxinc/nginx-unprivileged:alpine

# Copy the build output to nginx
COPY dist /usr/share/nginx/html

# nginx.conf is an envsubst template: the image entrypoint renders it to
# /etc/nginx/conf.d/default.conf at startup, substituting ${GRAM_CDN_URL}.
COPY nginx.conf /etc/nginx/templates/default.conf.template

# Default: no CDN rewrite. Must always be defined — envsubst only substitutes
# variables present in the environment, and an unsubstituted ${GRAM_CDN_URL}
# would be parsed by nginx as an (undefined) nginx variable and fail startup.
ENV GRAM_CDN_URL=""

EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]