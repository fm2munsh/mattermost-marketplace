FROM node:8-alpine

RUN apk add --update curl python-dev py-pip screen gpgme make && \
    pip install --upgrade pip && \
    rm -rf /var/cache/apk/*

RUN curl -o- -L https://yarnpkg.com/install.sh | sh
ENV PATH /root/.yarn/bin:$PATH

RUN pip install awscli

ARG SERVERLESS_VERSION=1.28.0

RUN npm install serverless@$SERVERLESS_VERSION -g --silent
