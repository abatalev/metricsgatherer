#!/bin/sh
CDIR=$(pwd)
#LINTER="1.62.2"
#LINTER="1.63.4"
#LINTER="1.55.0"
LINTER="2.0.2"
export CGO_ENABLED=0

OS_NAME=$(cat /etc/os-release | awk -F= '/^NAME=/{ print $2; }')
OS_VERSION=$(cat /etc/os-release | awk -F= '/^VERSION_ID=/{ print $2; }')
echo "### os ${OS_NAME} ${OS_VERSION}"

GO_VERSION=$(gawk '/^go/{ print $2; }' ./go.mod)
GO_INSTALLED=$(go version| gawk '{print $3; }')
GOBIN="go${GO_VERSION}"

echo "### required go version go${GO_VERSION}"
echo "### installed go version ${GO_INSTALLED}"

GO_DOWN_ENABLE=$(echo " ${GO_INSTALLED}" | grep "go${GO_VERSION}" | wc -l)

if [ "${GO_DOWN_ENABLE}" = 1 ]; then
    GOBIN="go"
else
    if ! command -v "go${GO_VERSION}" > /dev/null; then
      echo "### go install go${GO_VERSION}"
      go install "golang.org/dl/go${GO_VERSION}@latest"
      GOBIN=$(command -v "go${GO_VERSION}")
      ${GOBIN} download
    fi
    export GOROOT="$(${GOBIN} env GOROOT)"
fi

build_app_git() {
    GIT_HASH=$1
    if [ -f "./tools/prj2hash" ]; then
        P2H_HASH=$(./tools/prj2hash)
    fi
    ${GOBIN} build -ldflags "-X main.gitHash=${GIT_HASH} -X main.p2hHash=${P2H_HASH}" .
#     if [ "${P2H_HASH}" = "" ]; then
#         P2H_HASH=$(./tools/prj2hash)
#         ${GOBIN} build -ldflags "-X main.gitHash=${GIT_HASH} -X main.p2hHash=${P2H_HASH}" .
#     fi
}

if [ ! -d "tools" ]; then
    mkdir tools
fi 
if [ ! -f "tools/gototcov" ]; then
    cd tools || exit
    git clone https://github.com/jonaz/gototcov gototcov.git
    cd gototcov.git || exit
    go get golang.org/x/tools/cover
    go build .
    cp gototcov.git ../gototcov
    cd ${CDIR}/tools || exit
    rm -f -R gototcov.git
    echo "### done build gototcov"
fi

echo "go root: ${GOROOT}"
cd $CDIR || exit
if [ ! -f "tools/golangci-lint" ]; then
  echo "### install golangci-lint $LINTER"
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b tools/ "v$LINTER"

  tools/golangci-lint --version
  echo "### done install golangci-lint"
else 
  XLINTER=$(tools/golangci-lint version --short)
  if [ "$XLINTER" != "$LINTER" ]; then
    rm tools/golangci-lint
    echo "### renstall golangci-lint $LINTER"
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b tools/ "v$LINTER"

    tools/golangci-lint --version
    echo "### done reinstall golangci-lint"
  fi  
fi

cd ${CDIR} || exit
echo "### mod tidy"
${GOBIN} mod tidy
${GOBIN} mod download

cd $CDIR || exit
echo "### linters"
if ! ./tools/golangci-lint run; then
    echo "### aborted linters"
    exit 1
fi

echo "### calc coverage"
if ! ${GOBIN} test -coverprofile=coverage.out .; then
    echo "### aborted"
    exit 1
fi

echo "### total coverage"
if ! ./tools/gototcov -f coverage.out -limit 50; then
    echo "### open browser"
    ${GOBIN} tool cover -html=coverage.out
    echo "### aborted"
    exit 1
fi

echo "### build application"
build_app_git "$(git rev-list -1 HEAD)"

# echo "### run new version info"
# ./prj2hash -version
echo "### the end"
