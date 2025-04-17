FROM golang:alpine

RUN apk update && apk upgrade && \
    apk add --no-cache \
    bash \
    git \
    curl \
    tar \
    xz \
    pkgconfig \
    build-base \
    libheif \
    libheif-dev

WORKDIR /bot

# download and extract FFmpeg 7.1 (GPL shared build)
RUN curl -L -o ffmpeg.tar.xz https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linux64-gpl-shared.tar.xz && \
    tar -xf ffmpeg.tar.xz && \
    rm ffmpeg.tar.xz && \
    cp -r ffmpeg-master-latest-linux64-gpl-shared/bin/* /usr/local/bin/ && \
    cp -r ffmpeg-master-latest-linux64-gpl-shared/lib/* /usr/local/lib/ && \
    cp -r ffmpeg-master-latest-linux64-gpl-shared/include/* /usr/local/include/ && \
    cp -r ffmpeg-master-latest-linux64-gpl-shared/lib/pkgconfig/* /usr/local/lib/pkgconfig/ && \
    ldconfig

ENV CGO_CFLAGS="-I/usr/local/include"
ENV CGO_LDFLAGS="-L/usr/local/lib"
ENV PKG_CONFIG_PATH="/usr/local/lib/pkgconfig"

RUN pkg-config --libs libavcodec

COPY . .

RUN mkdir -p downloads
RUN chmod +x build.sh
RUN ./build.sh

ENTRYPOINT ["./govd"]
