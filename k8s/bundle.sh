#!/bin/sh
# k8s/bundle.sh — turn a servicetalk bundle into a Kubernetes deploy.
#
# Picks up where ./bundle.sh leaves off.  Builds the docker image,
# (optionally) pushes it, and writes a tailored Kubernetes manifest
# next to the bundle so you can `kubectl apply -f` and go.
#
# Workflow:
#
#   ./bundle.sh         myapp     dist/myapp
#   ./k8s/bundle.sh     dist/myapp    registry.example/myapp:v1
#   docker push                       registry.example/myapp:v1   # if --push not used
#   kubectl apply -f    dist/myapp/kubernetes.yaml
#   kubectl port-forward svc/myapp-coordinator 8000:8000
#
# Usage:
#   k8s/bundle.sh BUNDLE_DIR IMAGE_REF [options]
#
# Required:
#   BUNDLE_DIR    a directory previously produced by ./bundle.sh
#                 (must contain Dockerfile, run, bin/).
#   IMAGE_REF     the full image reference to build/use, e.g.
#                 ghcr.io/you/myapp:v1 or 10.0.0.5:5000/myapp:dev.
#
# Options:
#   --workers N         worker replica count (default 3)
#   --namespace NS      target namespace (default: omit; deploys to
#                       the current kubectl context's default ns)
#   --name NAME         resource-name prefix (default: derived from
#                       IMAGE_REF — the repo basename, e.g. "myapp"
#                       from "ghcr.io/you/myapp:v1")
#   --platforms LIST    comma-separated buildx platforms, e.g.
#                       linux/amd64,linux/arm64.  Triggers buildx
#                       (and requires --push, since multi-arch
#                       images can't be loaded into the local docker
#                       daemon).
#   --build / --no-build    build the image (default: --build)
#   --push                  docker push (or buildx --push) after build
#   --apply                 run `kubectl apply -f` after writing
#   --out FILE              manifest output path (default:
#                           BUNDLE_DIR/kubernetes.yaml)
#   --type TYPE             coordinator Service type:
#                             ClusterIP    (default; in-cluster only)
#                             NodePort     (port 30080 by default)
#                             LoadBalancer (cloud)
#   --node-port PORT        nodePort to use when --type=NodePort
#                           (default 30080; valid range 30000-32767)
#   -h, --help              this message
#
# Exit codes:
#   0  success
#   1  filesystem / build / kubectl error
#   2  bad usage

set -eu

usage() {
    sed -n '/^# /,/^$/p' "$0" | sed 's/^# \{0,1\}//;s/^#$//'
    exit "${1:-2}"
}

BUNDLE_DIR=""
IMAGE_REF=""
WORKERS=3
NAMESPACE=""
NAME=""
PLATFORMS=""
BUILD=1
PUSH=0
APPLY=0
OUT=""
SVC_TYPE=ClusterIP
NODE_PORT=30080

while [ $# -gt 0 ]; do
    case "$1" in
        --workers)    WORKERS="$2"; shift 2 ;;
        --namespace)  NAMESPACE="$2"; shift 2 ;;
        --name)       NAME="$2"; shift 2 ;;
        --platforms)  PLATFORMS="$2"; shift 2 ;;
        --build)      BUILD=1; shift ;;
        --no-build)   BUILD=0; shift ;;
        --push)       PUSH=1; shift ;;
        --apply)      APPLY=1; shift ;;
        --out)        OUT="$2"; shift 2 ;;
        --type)       SVC_TYPE="$2"; shift 2 ;;
        --node-port)  NODE_PORT="$2"; shift 2 ;;
        -h|--help)    usage 0 ;;
        --*)          echo "k8s/bundle.sh: unknown flag: $1" >&2; usage ;;
        *)
            if   [ -z "$BUNDLE_DIR" ]; then BUNDLE_DIR="$1"
            elif [ -z "$IMAGE_REF" ];  then IMAGE_REF="$1"
            else echo "k8s/bundle.sh: extra arg: $1" >&2; usage
            fi
            shift ;;
    esac
done

[ -n "$BUNDLE_DIR" ] || usage
[ -n "$IMAGE_REF" ]  || usage
[ -d "$BUNDLE_DIR" ] || { echo "k8s/bundle.sh: $BUNDLE_DIR is not a directory" >&2; exit 1; }
[ -f "$BUNDLE_DIR/Dockerfile" ] || { echo "k8s/bundle.sh: $BUNDLE_DIR has no Dockerfile — run ./bundle.sh first" >&2; exit 1; }

case "$SVC_TYPE" in
    ClusterIP|NodePort|LoadBalancer) ;;
    *) echo "k8s/bundle.sh: --type must be ClusterIP, NodePort, or LoadBalancer" >&2; exit 2 ;;
esac

if [ -z "$NAME" ]; then
    NAME=$(printf '%s\n' "$IMAGE_REF" | sed 's|:.*||; s|.*/||')
fi
[ -n "$NAME" ] || { echo "k8s/bundle.sh: could not derive --name from $IMAGE_REF" >&2; exit 1; }

[ -n "$OUT" ] || OUT="$BUNDLE_DIR/kubernetes.yaml"

if [ -n "$PLATFORMS" ] && [ "$BUILD" = 1 ] && [ "$PUSH" = 0 ]; then
    echo "k8s/bundle.sh: --platforms requires --push (buildx multi-arch can't load into the local daemon)" >&2
    exit 2
fi

if [ "$BUILD" = 1 ]; then
    if [ -n "$PLATFORMS" ]; then
        echo "→ docker buildx build --platform $PLATFORMS --push -t $IMAGE_REF $BUNDLE_DIR"
        docker buildx build --platform "$PLATFORMS" --push -t "$IMAGE_REF" "$BUNDLE_DIR"
    else
        echo "→ docker build -t $IMAGE_REF $BUNDLE_DIR"
        docker build -t "$IMAGE_REF" "$BUNDLE_DIR"
        if [ "$PUSH" = 1 ]; then
            echo "→ docker push $IMAGE_REF"
            docker push "$IMAGE_REF"
        fi
    fi
fi

NS_FIELD=""
if [ -n "$NAMESPACE" ]; then
    NS_FIELD="
  namespace: $NAMESPACE"
fi

NODE_PORT_LINE=""
if [ "$SVC_TYPE" = NodePort ]; then
    NODE_PORT_LINE="
      nodePort: $NODE_PORT"
fi

cat > "$OUT" <<EOF
# Generated by k8s/bundle.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ).
# Apply:    kubectl apply -f $(basename "$OUT")
# Status:   kubectl get pods -l app=$NAME
# REPL:     kubectl port-forward svc/$NAME-coordinator 8000:8000
# Scale:    kubectl scale deployment $NAME-worker --replicas=N
# Tear down:kubectl delete -f $(basename "$OUT")
#
# Regenerate this file by re-running k8s/bundle.sh with the new args.

---
apiVersion: v1
kind: Service
metadata:
  name: $NAME-nats$NS_FIELD
  labels:
    app: $NAME
    role: nats
spec:
  selector:
    app: $NAME
    role: nats
  ports:
    - name: nats
      port: 4222
      targetPort: 4222

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $NAME-nats$NS_FIELD
  labels:
    app: $NAME
    role: nats
spec:
  replicas: 1
  selector:
    matchLabels:
      app: $NAME
      role: nats
  template:
    metadata:
      labels:
        app: $NAME
        role: nats
    spec:
      containers:
        - name: nats
          image: nats:2
          args: ["-js"]
          ports:
            - name: nats
              containerPort: 4222

---
apiVersion: v1
kind: Service
metadata:
  name: $NAME-coordinator$NS_FIELD
  labels:
    app: $NAME
    role: coordinator
spec:
  type: $SVC_TYPE
  selector:
    app: $NAME
    role: coordinator
  ports:
    - name: http
      port: 8000
      targetPort: 8000$NODE_PORT_LINE

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $NAME-coordinator$NS_FIELD
  labels:
    app: $NAME
    role: coordinator
spec:
  replicas: 1
  strategy:
    type: Recreate              # registry is in-memory; no rolling
  selector:
    matchLabels:
      app: $NAME
      role: coordinator
  template:
    metadata:
      labels:
        app: $NAME
        role: coordinator
    spec:
      containers:
        - name: coordinator
          image: $IMAGE_REF
          imagePullPolicy: IfNotPresent
          command: ["/app/run"]
          args:
            - coordinator
            - --port=8000
            - --main=
          env:
            - name: SMT_HOST
              value: $NAME-coordinator
            - name: SMT_NATS_URL
              value: nats://$NAME-nats:4222
            - name: SMT_CLUSTER
              value: $NAME
            - name: SMT_NATS_STARTUP_TIMEOUT_MS
              value: "60000"
            - name: SMT_SPAWN_TIMEOUT_MS
              value: "75000"
            - name: SMT_MAILBOX_SIZE
              value: "1024"
            - name: SMT_CONTROL_MAILBOX_SIZE
              value: "128"
            - name: SMT_NATS_MAX_CALL_BYTES
              value: "1048576"
            - name: SMT_INTERNAL_HTTP_MAX_BYTES
              value: "335544320"
            - name: SMT_SOURCE_BUNDLE_MAX_BYTES
              value: "268435456"
            - name: SMT_SOURCE_BUNDLE_MAX_FILES
              value: "10000"
            - name: SMT_FOREIGN_CC
              value: cc
            - name: SMT_FOREIGN_CARGO
              value: cargo
            - name: SMT_FOREIGN_CFLAGS
              value: ""
            - name: SMT_FOREIGN_LDFLAGS
              value: ""
            - name: SMT_FOREIGN_RUST_TARGET
              value: ""
          ports:
            - name: http
              containerPort: 8000
          readinessProbe:
            httpGet:
              path: /registry
              port: http
            initialDelaySeconds: 1
            periodSeconds: 5
          livenessProbe:
            httpGet:
              path: /registry
              port: http
            initialDelaySeconds: 10
            periodSeconds: 20

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $NAME-worker$NS_FIELD
  labels:
    app: $NAME
    role: worker
spec:
  replicas: $WORKERS
  selector:
    matchLabels:
      app: $NAME
      role: worker
  template:
    metadata:
      labels:
        app: $NAME
        role: worker
    spec:
      containers:
        - name: worker
          image: $IMAGE_REF
          imagePullPolicy: IfNotPresent
          command: ["/app/run"]
          args:
            - worker
            - --coordinator=http://$NAME-coordinator:8000
            - --port=8001
          env:
            - name: SMT_HOST
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: SMT_NATS_URL
              value: nats://$NAME-nats:4222
            - name: SMT_CLUSTER
              value: $NAME
            - name: SMT_NATS_STARTUP_TIMEOUT_MS
              value: "60000"
            - name: SMT_SPAWN_TIMEOUT_MS
              value: "75000"
            - name: SMT_MAILBOX_SIZE
              value: "1024"
            - name: SMT_CONTROL_MAILBOX_SIZE
              value: "128"
            - name: SMT_NATS_MAX_CALL_BYTES
              value: "1048576"
            - name: SMT_INTERNAL_HTTP_MAX_BYTES
              value: "335544320"
            - name: SMT_SOURCE_BUNDLE_MAX_BYTES
              value: "268435456"
            - name: SMT_SOURCE_BUNDLE_MAX_FILES
              value: "10000"
            - name: SMT_FOREIGN_CC
              value: cc
            - name: SMT_FOREIGN_CARGO
              value: cargo
            - name: SMT_FOREIGN_CFLAGS
              value: ""
            - name: SMT_FOREIGN_LDFLAGS
              value: ""
            - name: SMT_FOREIGN_RUST_TARGET
              value: ""
          ports:
            - name: worker
              containerPort: 8001
          readinessProbe:
            httpGet:
              path: /ping
              port: worker
            initialDelaySeconds: 1
            periodSeconds: 5
EOF

echo
echo "wrote $OUT"
echo

if [ "$APPLY" = 1 ]; then
    echo "→ kubectl apply -f $OUT"
    if [ -n "$NAMESPACE" ]; then
        kubectl apply -n "$NAMESPACE" -f "$OUT"
    else
        kubectl apply -f "$OUT"
    fi
    echo
    echo "next:"
    echo "  kubectl port-forward svc/$NAME-coordinator 8000:8000"
else
    echo "next:"
    if [ "$BUILD" = 1 ] && [ "$PUSH" = 0 ] && [ -z "$PLATFORMS" ]; then
        echo "  docker push $IMAGE_REF"
    fi
    echo "  kubectl apply -f $OUT"
    echo "  kubectl port-forward svc/$NAME-coordinator 8000:8000"
fi
