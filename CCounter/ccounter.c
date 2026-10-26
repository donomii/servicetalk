/* CCounter — a minimal counter whose state lives in this C process.
 *
 * From the outside this is indistinguishable from the S-expression-based
 * Counter class: `(new CCounter)` spawns this binary as a subprocess,
 * `(send c increment)` routes through the bridge, and every message is
 * dispatched to one of the functions below via _st_glue.c.
 */

#include "_st.h"

static double count = 0;

Value ccounter_initialize(int nargs, Value* args) {
    (void)nargs; (void)args;
    count = 0;
    return st_nil();
}

Value ccounter_increment(int nargs, Value* args) {
    (void)nargs; (void)args;
    count += 1;
    return st_number(count);
}

Value ccounter_decrement(int nargs, Value* args) {
    (void)nargs; (void)args;
    count -= 1;
    return st_number(count);
}

Value ccounter_value(int nargs, Value* args) {
    (void)nargs; (void)args;
    return st_number(count);
}

Value ccounter_add(int nargs, Value* args) {
    if (nargs < 1 || !st_isNumber(args[0])) {
        st_signal("CCounter add: expects a number");
    }
    count += st_asNumber(args[0]);
    return st_number(count);
}

/* Given another object, return its class name by sending `class` to it.
 * Demonstrates st_send from C: a foreign class interoperating with the
 * rest of the ServiceTalk object graph. */
Value ccounter_classOf(int nargs, Value* args) {
    if (nargs < 1 || !st_isObject(args[0])) {
        st_signal("CCounter classOf: expects an object");
    }
    return st_send(args[0], "class", 0, (Value*)0);
}
