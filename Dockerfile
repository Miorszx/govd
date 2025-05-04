FROM golang:bookworm

# set build arguments
ARG FFMPEG_VERSION=7.1
ARG LIBHEIF_VERSION=1.19.7

# environment variables for build
ENV CGO_CFLAGS="-I/usr/local/include"
ENV CGO_LDFLAGS="-L/usr/local/lib"
ENV PKG_CONFIG_PATH="/usr/local/lib/pkgconfig"

WORKDIR /bot

COPY . .

# ensure the .env file exists
RUN test -f .env || (echo ".env file is missing. build aborted." && exit 1)

RUN mkdir -p downloads

WORKDIR /bot/packages

# install build dependencies
RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends \
        bash \
        git \
        pkg-config \
        build-essential \
        tar \
        wget \
        xz-utils \
        gcc \
        cmake \
        libde265-dev && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# build and install libheif
RUN wget -O libheif.tar.gz "https://github.com/strukturag/libheif/releases/download/v${LIBHEIF_VERSION}/libheif-${LIBHEIF_VERSION}.tar.gz" && \
    mkdir libheif && \
    tar -xzvf libheif.tar.gz -C libheif --strip-components=1 && \
    rm libheif.tar.gz && \
    cd libheif && \
    mkdir build && \
    cd build && \
    cmake --preset=release .. && \
    make -j"$(nproc)" && \
    make install

# prepare directories for ffmpeg
RUN mkdir -p \
    /usr/local/bin \
    /usr/local/lib/pkgconfig \
    /usr/local/lib \
    /usr/local/include

# download and install ffmpeg (arch-aware)
RUN ARCH="$(uname -m)" && \
    if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then \
        FFMPEG_BUILD="https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-n${FFMPEG_VERSION}-latest-linuxarm64-gpl-shared-${FFMPEG_VERSION}.tar.xz"; \
    else \
        FFMPEG_BUILD="https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-n${FFMPEG_VERSION}-latest-linux64-gpl-shared-${FFMPEG_VERSION}.tar.xz"; \
    fi && \
    wget -O ffmpeg.tar.xz "${FFMPEG_BUILD}" && \
    mkdir ffmpeg && \
    tar -xf ffmpeg.tar.xz -C ffmpeg --strip-components=1 && \
    rm ffmpeg.tar.xz && \
    cp -rv ffmpeg/bin/* /usr/local/bin/ && \
    cp -rv ffmpeg/lib/* /usr/local/lib/ && \
    cp -rv ffmpeg/include/* /usr/local/include/ && \
    cp -rv ffmpeg/lib/pkgconfig/* /usr/local/lib/pkgconfig/ && \
    ldconfig /usr/local

WORKDIR /bot

RUN chmod +x build.sh && ./build.sh

ENTRYPOINT ["./govd"]